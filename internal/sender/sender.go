// Package sender contains the background scheduler that generates and sends
// campaign emails, respecting per-account daily caps and sending windows.
package sender

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"github.com/zainclaude/goutreach/internal/ai"
	"github.com/zainclaude/goutreach/internal/mailauth"
	"github.com/zainclaude/goutreach/internal/mailer"
	"github.com/zainclaude/goutreach/internal/store"
)

// Service generates and sends campaign emails.
type Service struct {
	st     *store.Store
	res    *mailauth.Resolver
	gen    *ai.Generator
	appURL string
	log    *log.Logger
}

// New builds a sender Service.
func New(st *store.Store, res *mailauth.Resolver, gen *ai.Generator, appURL string, logger *log.Logger) *Service {
	return &Service{st: st, res: res, gen: gen, appURL: appURL, log: logger}
}

// Run starts the scheduler loop until the context is cancelled.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Service) tick(ctx context.Context) {
	due, err := s.st.DueCampaignLeads(ctx, 25)
	if err != nil {
		s.log.Printf("sender: due query: %v", err)
		return
	}
	for _, cl := range due {
		if err := s.processLead(ctx, cl); err != nil {
			s.log.Printf("sender: lead %d: %v", cl.ID, err)
		}
		// Small jitter between sends to look human and spread load.
		time.Sleep(time.Duration(500+rand.Intn(1500)) * time.Millisecond)
	}
	// Sweep every running campaign (not just ones touched this tick) so any whose
	// leads have all finished flips to completed — including ones that finished
	// before this check existed and so never appear in the due queue again.
	if n, err := s.st.CompleteFinishedCampaigns(ctx); err != nil {
		s.log.Printf("sender: complete sweep: %v", err)
	} else if n > 0 {
		s.log.Printf("sender: marked %d campaign(s) completed (all leads finished)", n)
	}
}

func (s *Service) processLead(ctx context.Context, cl store.CampaignLead) error {
	campaign, err := s.st.GetCampaignByID(ctx, cl.CampaignID)
	if err != nil {
		return fmt.Errorf("get campaign: %w", err)
	}
	if campaign.Status != "running" {
		return nil
	}
	// While approval is required, the loop does not send — previews are produced
	// via the explicit preview endpoint and the user must launch the campaign.
	if campaign.RequireApproval {
		return nil
	}

	now := time.Now()
	if !inSendWindow(campaign, now) {
		next := nextWindowOpen(campaign, now)
		return s.st.AdvanceCampaignLead(ctx, cl.ID, cl.CurrentStep, next, "active")
	}

	// Skip blacklisted (domain) or known-invalid (failed verification) leads at
	// send time — re-checked here in case the status changed after enrollment.
	if lead, err := s.st.GetLead(ctx, cl.LeadID); err == nil {
		if bl, err := s.st.BlacklistedDomains(ctx, campaign.UserID); err == nil && store.IsBlacklisted(bl, lead.Email) {
			s.log.Printf("sender: skipping blacklisted lead %s (campaign %d)", lead.Email, cl.CampaignID)
			return s.st.SetCampaignLeadStatus(ctx, cl.ID, "skipped")
		}
		if lead.Verification == "invalid" {
			s.log.Printf("sender: skipping invalid (unverifiable) lead %s (campaign %d)", lead.Email, cl.CampaignID)
			return s.st.SetCampaignLeadStatus(ctx, cl.ID, "skipped")
		}
	}

	step, err := s.st.GetStep(ctx, cl.CampaignID, cl.CurrentStep)
	if err != nil {
		// No such step => sequence finished.
		return s.st.SetCampaignLeadStatus(ctx, cl.ID, "finished")
	}

	// Choose an eligible inbox (under its daily cap, active).
	account, ok, err := s.pickAccount(ctx, cl.CampaignID)
	if err != nil {
		return fmt.Errorf("pick account: %w", err)
	}
	if !ok {
		// All inboxes capped for today; retry in an hour.
		return s.st.AdvanceCampaignLead(ctx, cl.ID, cl.CurrentStep, now.Add(time.Hour), "active")
	}

	msg, err := s.ensureGenerated(ctx, campaign, cl, step, account.FromName)
	if err != nil {
		return err
	}
	if !msg.Approved {
		// Shouldn't happen with require_approval=false, but guard anyway.
		return nil
	}

	if err := s.send(ctx, campaign, cl, step, account, msg); err != nil {
		_ = s.st.SetMessageFailed(ctx, msg.ID, err.Error())
		return err
	}
	return s.advance(ctx, cl, step)
}

// ensureGenerated returns the message for (lead, step), generating its content if needed.
func (s *Service) ensureGenerated(ctx context.Context, campaign store.Campaign, cl store.CampaignLead, step store.CampaignStep, fromName string) (store.Message, error) {
	msg, err := s.st.GetMessageForStep(ctx, cl.ID, step.StepIndex)
	if err != nil {
		// create new
		msg, err = s.st.CreateMessage(ctx, store.Message{
			CampaignLeadID: cl.ID,
			StepIndex:      step.StepIndex,
			Status:         "queued",
			Approved:       !campaign.RequireApproval,
		})
		if err != nil {
			return store.Message{}, fmt.Errorf("create message: %w", err)
		}
	}
	if msg.Status == "generated" || msg.Status == "sent" {
		return msg, nil
	}

	lead, err := s.st.GetLead(ctx, cl.LeadID)
	if err != nil {
		return store.Message{}, fmt.Errorf("get lead: %w", err)
	}

	// Brand-level cache: if this domain was already routed for this step, reuse
	// the saved email (re-personalized for this contact) — no tokens, no wait.
	domain := emailDomain(lead.Email)
	cacheable := domain != "" && !isFreeMail(domain)
	if cacheable {
		if cached, ok, err := s.st.GetBrandEmail(ctx, campaign.UserID, domain, step.StepIndex); err != nil {
			s.log.Printf("sender: brand cache lookup %s: %v", domain, err)
		} else if ok {
			subject := ai.Personalize(cached.Subject, lead)
			body := ai.Personalize(cached.Body, lead)
			notes := fmt.Sprintf("cached brand email (%s, template %s)", domain, cached.TemplateUsed)
			if err := s.st.SetMessageGenerated(ctx, msg.ID, subject, body, cached.TemplateUsed, notes); err != nil {
				return store.Message{}, err
			}
			s.log.Printf("sender: reused cached email for %s (lead %d, step %d) — skipped generation", domain, lead.ID, step.StepIndex)
			return s.st.GetMessage(ctx, msg.ID)
		}
	}

	templates, err := s.st.ListTemplates(ctx, campaign.UserID)
	if err != nil {
		return store.Message{}, fmt.Errorf("templates: %w", err)
	}

	res, err := s.gen.Generate(ctx, ai.Input{
		Brief:     campaign.Brief,
		Angle:     step.Angle,
		Lead:      lead,
		Templates: templates,
		FromName:  fromName,
	})
	if err != nil {
		_ = s.st.SetMessageFailed(ctx, msg.ID, err.Error())
		return store.Message{}, fmt.Errorf("generate: %w", err)
	}
	notes := fmt.Sprintf("template %s: %s", res.TemplateUsed, res.Reasoning)
	if err := s.st.SetMessageGenerated(ctx, msg.ID, res.Subject, res.Body, res.TemplateUsed, notes); err != nil {
		return store.Message{}, err
	}

	// Cache this brand's email so future contacts at the same domain reuse it.
	if cacheable {
		brandName := strings.TrimSpace(lead.Company)
		if brandName == "" {
			brandName = domain
		}
		if err := s.st.SaveBrandEmail(ctx, store.BrandEmail{
			UserID: campaign.UserID, Domain: domain, StepIndex: step.StepIndex,
			BrandName: brandName, TemplateUsed: res.TemplateUsed,
			Subject:       ai.TokenizeName(res.Subject, lead),
			Body:          ai.TokenizeName(res.Body, lead),
			ResearchNotes: res.Reasoning,
		}); err != nil {
			s.log.Printf("sender: brand cache save %s: %v", domain, err)
		}
	}
	return s.st.GetMessage(ctx, msg.ID)
}

func (s *Service) send(ctx context.Context, campaign store.Campaign, cl store.CampaignLead, step store.CampaignStep, account store.EmailAccount, msg store.Message) error {
	lead, err := s.st.GetLead(ctx, cl.LeadID)
	if err != nil {
		return err
	}
	smtpCreds, err := s.res.SMTP(ctx, account)
	if err != nil {
		return fmt.Errorf("smtp creds: %w", err)
	}

	subject := msg.Subject
	var inReplyTo, references string
	if step.StepIndex > 0 {
		if prev, err := s.st.GetMessageForStep(ctx, cl.ID, step.StepIndex-1); err == nil && prev.MessageID != "" {
			inReplyTo = prev.MessageID
			references = strings.TrimSpace(prev.References + " " + prev.MessageID)
			if !strings.HasPrefix(strings.ToLower(subject), "re:") {
				subject = "Re: " + subject
			}
		}
	}

	htmlBody := buildHTML(msg.Body, s.appURL, msg.ID, campaign.TrackOpens, campaign.TrackClicks)

	messageID, err := mailer.Send(ctx, smtpCreds, mailer.OutgoingEmail{
		FromAddr:  account.Email,
		FromName:  account.FromName,
		ToAddr:    lead.Email,
		ToName:    strings.TrimSpace(lead.FirstName + " " + lead.LastName),
		Subject:   subject,
		HTMLBody:  htmlBody,
		TextBody:  msg.Body,
		InReplyTo: inReplyTo,
		Refs:      references,
	})
	if err != nil {
		return err
	}
	return s.st.SetMessageSent(ctx, msg.ID, account.ID, messageID, inReplyTo, references)
}

func (s *Service) advance(ctx context.Context, cl store.CampaignLead, step store.CampaignStep) error {
	next, err := s.st.GetStep(ctx, cl.CampaignID, step.StepIndex+1)
	if err != nil {
		return s.st.SetCampaignLeadStatus(ctx, cl.ID, "finished")
	}
	when := time.Now().Add(time.Duration(next.DelayDays) * 24 * time.Hour)
	return s.st.AdvanceCampaignLead(ctx, cl.ID, next.StepIndex, when, "active")
}

// emailDomain returns the lowercased domain part of an email ("" if malformed).
func emailDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(email[at+1:]))
}

// freeMailDomains are public mailbox providers — not brand domains — so we never
// cluster unrelated leads under them in the brand cache.
var freeMailDomains = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "yahoo.com": true, "ymail.com": true,
	"hotmail.com": true, "outlook.com": true, "live.com": true, "msn.com": true,
	"icloud.com": true, "me.com": true, "mac.com": true, "aol.com": true,
	"proton.me": true, "protonmail.com": true, "gmx.com": true, "zoho.com": true,
}

func isFreeMail(domain string) bool { return freeMailDomains[domain] }

// pickAccount returns the next inbox in strict round-robin order: the eligible
// account (active, under its daily cap) that has sent the fewest emails today,
// ties broken by account id. This sends 1 per account and cycles through the
// pool, distributing remainder evenly.
func (s *Service) pickAccount(ctx context.Context, campaignID int64) (store.EmailAccount, bool, error) {
	ids, err := s.st.ListCampaignAccountIDs(ctx, campaignID)
	if err != nil {
		return store.EmailAccount{}, false, err
	}
	var best store.EmailAccount
	bestSent := -1
	found := false
	for _, id := range ids {
		acc, err := s.st.GetAccountByID(ctx, id)
		if err != nil || acc.Status != "active" {
			continue
		}
		sent, err := s.st.CountSentToday(ctx, acc.ID)
		if err != nil || sent >= acc.DailyLimit {
			continue
		}
		if !found || sent < bestSent || (sent == bestSent && acc.ID < best.ID) {
			best = acc
			bestSent = sent
			found = true
		}
	}
	return best, found, nil
}

// GeneratePreview generates (without sending) the message for a lead+step. Used
// by the preview endpoint so the user can review the first emails before launch.
func (s *Service) GeneratePreview(ctx context.Context, campaign store.Campaign, cl store.CampaignLead, stepIndex int) (store.Message, error) {
	step, err := s.st.GetStep(ctx, campaign.ID, stepIndex)
	if err != nil {
		return store.Message{}, fmt.Errorf("step %d not found", stepIndex)
	}
	fromName := ""
	if acc, ok, _ := s.pickAccount(ctx, campaign.ID); ok {
		fromName = acc.FromName
	}
	return s.ensureGenerated(ctx, campaign, cl, step, fromName)
}
