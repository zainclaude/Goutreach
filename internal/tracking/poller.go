package tracking

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"regexp"
	"strings"
	"time"

	"github.com/zainclaude/goutreach/internal/ai"
	"github.com/zainclaude/goutreach/internal/beehiiv"
	"github.com/zainclaude/goutreach/internal/crypto"
	"github.com/zainclaude/goutreach/internal/mailauth"
	"github.com/zainclaude/goutreach/internal/mailer"
	"github.com/zainclaude/goutreach/internal/store"
)

// Poller periodically checks each account's IMAP inbox for replies and bounces,
// and registers warmup deliveries.
type Poller struct {
	st     *store.Store
	res    *mailauth.Resolver
	gen    *ai.Generator
	cipher *crypto.Cipher
	appURL string
	log    *log.Logger
}

// NewPoller builds a reply/bounce poller. gen classifies inbound replies;
// cipher decrypts stored integration secrets (beehiiv).
func NewPoller(st *store.Store, res *mailauth.Resolver, gen *ai.Generator, cipher *crypto.Cipher, appURL string, logger *log.Logger) *Poller {
	return &Poller{st: st, res: res, gen: gen, cipher: cipher, appURL: appURL, log: logger}
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
		errMsg := ""
		if err := p.pollAccount(ctx, acc); err != nil {
			errMsg = err.Error()
			p.log.Printf("poller: account %d (%s): %v", acc.ID, acc.Email, err)
		}
		// Record poll health (success clears the error) for the Accounts page.
		_ = p.st.RecordPollResult(ctx, acc.ID, errMsg)
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
			reason := bounceReason(in.Text)
			_ = p.st.SetMessageBounced(ctx, msg.ID, reason)
			// Security gateways often send a rejection notice from the recipient's
			// own address seconds before the real NDR; that notice gets recorded as
			// a reply. The bounce supersedes it — drop the bogus reply event so the
			// dashboard's reply count matches the inbox.
			_ = p.st.DeleteReplyEvents(ctx, msg.ID)
			p.log.Printf("poller: bounce msg %d via %s: %s", msg.ID, acc.Email, reason)
			if msg.Status != "bounced" {
				_ = p.st.CreateEvent(ctx, msg.ID, "bounce", map[string]any{"from": in.FromAddr, "reason": reason})
				if cl, err := p.st.GetCampaignLead(ctx, msg.CampaignLeadID); err == nil {
					_ = p.st.SetCampaignLeadStatus(ctx, cl.ID, "bounced")
					// A hard bounce proves the mailbox doesn't exist — mark the
					// lead invalid so no campaign ever emails it again.
					if isHardBounce(reason) {
						_ = p.st.SetLeadVerification(ctx, cl.LeadID, "invalid")
						p.log.Printf("poller: hard bounce — lead %d marked invalid (won't be emailed again)", cl.LeadID)
					}
				}
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

	// Capture the lead's reply text (idempotent — also backfills replies that
	// were detected before reply bodies were stored).
	if in.Text != "" {
		_ = p.st.SetReplyBody(ctx, msg.ID, in.Text)
	}
	if msg.Status == "replied" {
		// Another inbound message on a thread that already replied. The
		// sequence stopped for this lead at their first reply, so anything
		// arriving now is a real person continuing the conversation — either
		// answering the user's reply (which may have been sent from their own
		// mail client, invisible to us) or bumping the thread. Always alert,
		// unclassified: at this stage nothing from the lead is safe to drop.
		_ = p.st.CreateEvent(ctx, msg.ID, "reply", map[string]any{
			"from":     in.FromAddr,
			"subject":  in.Subject,
			"followup": true,
		})
		p.notifyReply(ctx, acc, in, true)
		return nil
	}

	_ = p.st.SetMessageStatus(ctx, msg.ID, "replied")
	_ = p.st.CreateEvent(ctx, msg.ID, "reply", map[string]any{
		"from":    in.FromAddr,
		"subject": in.Subject,
	})
	if cl, err := p.st.GetCampaignLead(ctx, msg.CampaignLeadID); err == nil {
		// Stop the sequence for a lead that replied.
		_ = p.st.SetCampaignLeadStatus(ctx, cl.ID, "replied")
	}
	// Classify the reply so OOO/unsubscribe noise is filtered and only
	// genuinely interested replies trigger a notification. If the classifier
	// is unavailable, fail open and notify — never silently drop a warm lead.
	category := ""
	if p.gen != nil {
		if cat, err := p.gen.ClassifyReply(ctx, in.Subject, in.Text); err == nil {
			category = cat
			_ = p.st.SetReplyCategory(ctx, msg.ID, cat)
			p.log.Printf("poller: reply from %s classified as %s", in.FromAddr, cat)
		} else {
			p.log.Printf("poller: reply classification failed (%v) — notifying anyway", err)
		}
	}
	if category == "interested" || category == "" {
		// Notify the user + team that there's a reply waiting (first detection only).
		p.notifyReply(ctx, acc, in, false)
	}
	if category == "interested" {
		p.subscribeInterested(ctx, acc.UserID, in.FromAddr)
	}
	return nil
}

// subscribeInterested adds an interested replier to the user's beehiiv
// newsletter. No-op unless a beehiiv API key + publication ID are configured
// in Settings; failures are logged, never fatal.
func (p *Poller) subscribeInterested(ctx context.Context, userID int64, email string) {
	pubID, ok, _ := p.st.GetSetting(ctx, userID, "beehiiv_publication_id")
	if !ok || strings.TrimSpace(pubID) == "" {
		return
	}
	enc, ok, _ := p.st.GetSetting(ctx, userID, "beehiiv_api_key")
	if !ok || enc == "" || p.cipher == nil {
		return
	}
	apiKey, err := p.cipher.Decrypt(enc)
	if err != nil {
		p.log.Printf("beehiiv: decrypt api key: %v", err)
		return
	}
	if err := beehiiv.Subscribe(ctx, apiKey, pubID, email); err != nil {
		p.log.Printf("beehiiv: subscribe %s failed: %v", email, err)
		return
	}
	p.log.Printf("beehiiv: added interested lead %s to the newsletter", email)
}

// notifyReply emails the user's configured notification addresses that a lead
// replied, so they can respond in PipelineBuilder. No-op if none are configured.
// followUp marks a reply in a thread the user already replied to themselves —
// an active conversation that likely needs a booking, so the alert says so.
func (p *Poller) notifyReply(ctx context.Context, acc store.EmailAccount, in mailer.InboundMessage, followUp bool) {
	raw, ok, _ := p.st.GetSetting(ctx, acc.UserID, "notification_emails")
	if !ok || strings.TrimSpace(raw) == "" {
		return
	}
	var to []string
	for _, a := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t' || r == '\r'
	}) {
		if a = strings.TrimSpace(a); a != "" {
			to = append(to, a)
		}
	}
	if len(to) == 0 {
		return
	}
	creds, err := p.res.SMTP(ctx, acc)
	if err != nil {
		p.log.Printf("notify: smtp creds %s: %v", acc.Email, err)
		return
	}
	preview := strings.TrimSpace(in.Text)
	if len(preview) > 500 {
		preview = preview[:500] + "…"
	}
	linkLine := ""
	if p.appURL != "" {
		linkLine = "\nReply here: " + strings.TrimRight(p.appURL, "/") + "/inbox\n"
	}
	headline := "You have a new reply in PipelineBuilder."
	subject := fmt.Sprintf("New reply from %s — respond in PipelineBuilder", in.FromAddr)
	if followUp {
		headline = "A lead you're in conversation with replied again — don't leave them waiting."
		subject = fmt.Sprintf("%s replied again — keep the conversation going", in.FromAddr)
	}
	body := fmt.Sprintf("%s\n\nFrom: %s\nSubject: %s\nInbox: %s\n\n%s\n%s",
		headline, in.FromAddr, in.Subject, acc.Email, preview, linkLine)
	if _, err := mailer.Send(ctx, creds, mailer.OutgoingEmail{
		FromAddr: acc.Email,
		FromName: "PipelineBuilder",
		ToAddr:   to[0],
		Cc:       to[1:],
		Subject:  subject,
		TextBody: body,
	}); err != nil {
		p.log.Printf("notify: send to %v: %v", to, err)
		return
	}
	p.log.Printf("notify: reply alert sent to %v (reply from %s)", to, in.FromAddr)
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

var smtpCodeRe = regexp.MustCompile(`\b[45]\d\d(\s+\d\.\d+\.\d+)?\b`)

// bounceReason extracts a concise human-readable reason from a bounce/NDR body:
// the line carrying an SMTP status code or a known failure phrase, else the start
// of the message. Best-effort — empty if nothing useful is found.
func bounceReason(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	phrases := []string{"does not exist", "user unknown", "no such user", "unknown recipient",
		"mailbox", "address not found", "not found", "disabled", "over quota", "quota exceeded",
		"blocked", "rejected", "spam", "policy", "unable to deliver", "recipient", "relay"}
	for _, ln := range strings.Split(text, "\n") {
		l := strings.TrimSpace(ln)
		if l == "" {
			continue
		}
		if smtpCodeRe.MatchString(l) {
			return clip(l, 240)
		}
		ll := strings.ToLower(l)
		for _, p := range phrases {
			if strings.Contains(ll, p) {
				return clip(l, 240)
			}
		}
	}
	return clip(strings.Join(strings.Fields(text), " "), 240)
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// isHardBounce reports whether an NDR reason indicates a permanently bad
// mailbox (as opposed to a soft failure like a full inbox or greylisting).
var hardBounceRe = regexp.MustCompile(`(?i)address not found|user unknown|no such user|unknown recipient|does not exist|recipient not found|invalid recipient|address rejected|mailbox (unavailable|not found)|5\.1\.1`)

func isHardBounce(reason string) bool { return hardBounceRe.MatchString(reason) }

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
