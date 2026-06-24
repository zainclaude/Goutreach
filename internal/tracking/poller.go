package tracking

import (
	"context"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/zainclaude/goutreach/internal/mailauth"
	"github.com/zainclaude/goutreach/internal/mailer"
	"github.com/zainclaude/goutreach/internal/store"
)

// Poller periodically checks each account's IMAP inbox for replies and bounces,
// and registers warmup deliveries.
type Poller struct {
	st  *store.Store
	res *mailauth.Resolver
	log *log.Logger
}

// NewPoller builds a reply/bounce poller.
func NewPoller(st *store.Store, res *mailauth.Resolver, logger *log.Logger) *Poller {
	return &Poller{st: st, res: res, log: logger}
}

// Run polls all active accounts on an interval until the context is cancelled.
func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	p.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.tick(ctx)
		}
	}
}

func (p *Poller) tick(ctx context.Context) {
	accounts, err := p.st.ListAllActiveAccounts(ctx)
	if err != nil {
		p.log.Printf("poller: list accounts: %v", err)
		return
	}
	for _, acc := range accounts {
		if err := p.pollAccount(ctx, acc); err != nil {
			p.log.Printf("poller: account %d (%s): %v", acc.ID, acc.Email, err)
		}
	}
}

func (p *Poller) pollAccount(ctx context.Context, acc store.EmailAccount) error {
	creds, err := p.res.IMAP(ctx, acc)
	if err != nil {
		return err
	}
	newUID, err := mailer.Poll(ctx, creds, uint32(acc.IMAPLastUID), 50, func(in mailer.InboundMessage, seen bool) error {
		return p.handleInbound(ctx, acc, in, seen)
	})
	if newUID > uint32(acc.IMAPLastUID) {
		_ = p.st.SetIMAPLastUID(ctx, acc.ID, int64(newUID))
	}
	if err != nil {
		return err
	}

	// Detect warmup mail that landed in spam, rescue it to the inbox, and record
	// the spam placement for the warmup results view.
	found, err := mailer.ScanSpamForWarmup(ctx, creds, func(mid string) bool {
		ok, _ := p.st.FindWarmupByMessageID(ctx, mid)
		return ok
	})
	if err != nil {
		p.log.Printf("poller: spam scan %s: %v", acc.Email, err)
		return nil
	}
	for _, mid := range found {
		_ = p.st.SetWarmupStatusByMessageID(ctx, mid, "spam")
	}
	return nil
}

func (p *Poller) handleInbound(ctx context.Context, acc store.EmailAccount, in mailer.InboundMessage, seen bool) error {
	// Warmup mail we sent: marking it Seen (handled by Poll) is the warmup "open".
	// Reply to a fraction of warmup mail to build two-way reputation.
	if in.MessageID != "" {
		mid := mailer.NormalizeMessageID(in.MessageID)
		if isWarmup, _ := p.st.FindWarmupByMessageID(ctx, mid); isWarmup {
			// It reached the inbox (the poller only reads INBOX) — record placement.
			_ = p.st.SetWarmupStatusByMessageID(ctx, mid, "received")
			// Only auto-reply to genuinely new (unread) warmup mail, so reprocessing
			// already-seen mail on a watermark backfill doesn't re-send replies.
			if !seen && rand.Float64() < 0.4 {
				p.replyWarmup(ctx, acc, in)
			}
			return nil
		}
	}

	candidates := splitRefs(in.InReplyTo, in.References)

	// Bounce detection: failures usually come from a mailer-daemon / postmaster
	// and reference the original message.
	if isBounce(in.FromAddr, in.Subject) {
		if msg, ok, _ := p.st.FindSentMessageByThread(ctx, acc.ID, candidates); ok {
			_ = p.st.SetMessageStatus(ctx, msg.ID, "bounced")
			_ = p.st.CreateEvent(ctx, msg.ID, "bounce", map[string]any{"from": in.FromAddr})
			if cl, err := p.st.GetCampaignLead(ctx, msg.CampaignLeadID); err == nil {
				_ = p.st.SetCampaignLeadStatus(ctx, cl.ID, "bounced")
			}
		}
		return nil
	}

	// Reply matching: prefer threading headers, fall back to most recent send to sender.
	msg, ok, _ := p.st.FindSentMessageByThread(ctx, acc.ID, candidates)
	if !ok {
		msg, ok, _ = p.st.LatestSentToRecipient(ctx, acc.ID, in.FromAddr)
	}
	if !ok {
		return nil // not related to our outreach
	}

	if msg.Status != "replied" {
		_ = p.st.SetMessageStatus(ctx, msg.ID, "replied")
		_ = p.st.CreateEvent(ctx, msg.ID, "reply", map[string]any{
			"from":    in.FromAddr,
			"subject": in.Subject,
		})
		if cl, err := p.st.GetCampaignLead(ctx, msg.CampaignLeadID); err == nil {
			// Stop the sequence for a lead that replied.
			_ = p.st.SetCampaignLeadStatus(ctx, cl.ID, "replied")
		}
	}
	return nil
}

// replyWarmup sends a short reply to a received warmup message from this account.
func (p *Poller) replyWarmup(ctx context.Context, acc store.EmailAccount, in mailer.InboundMessage) {
	creds, err := p.res.SMTP(ctx, acc)
	if err != nil {
		return
	}
	subject := in.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	replies := []string{
		"Sounds good, thanks!", "Got it — appreciate the note.",
		"Perfect, talk soon.", "Thanks for the update!", "Yes, let's do it.",
	}
	_, _ = mailer.Send(ctx, creds, mailer.OutgoingEmail{
		FromAddr:  acc.Email,
		FromName:  acc.FromName,
		ToAddr:    in.FromAddr,
		Subject:   subject,
		TextBody:  replies[rand.Intn(len(replies))],
		InReplyTo: in.MessageID,
	})
}

func splitRefs(parts ...string) []string {
	var out []string
	for _, p := range parts {
		for _, tok := range strings.Fields(p) {
			tok = strings.TrimSpace(tok)
			if tok != "" {
				out = append(out, tok)
			}
		}
	}
	return out
}

func isBounce(from, subject string) bool {
	f := strings.ToLower(from)
	s := strings.ToLower(subject)
	if strings.Contains(f, "mailer-daemon") || strings.Contains(f, "postmaster") {
		return true
	}
	for _, kw := range []string{"delivery status notification", "undeliverable",
		"delivery has failed", "mail delivery failed", "returned mail", "failure notice"} {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}
