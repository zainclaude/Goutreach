package store

import (
	"context"
	"time"
)

// ReplyThread represents a lead who replied, for the unified inbox.
type ReplyThread struct {
	MessageID    int64      `json:"message_id"`     // our DB message id
	AccountID    *int64     `json:"account_id"`     // inbox that sent it
	RFCMessageID string     `json:"rfc_message_id"` // Message-ID for threading
	Subject      string     `json:"subject"`
	Body         string     `json:"body"`
	SentAt       *time.Time `json:"sent_at"`
	RepliedAt    *time.Time `json:"replied_at"`
	LeadEmail    string     `json:"lead_email"`
	LeadName     string     `json:"lead_name"`
	CampaignID   int64      `json:"campaign_id"`
	CampaignName string     `json:"campaign_name"`
	ReplySnippet string     `json:"reply_snippet"`
	ReplyBody    string     `json:"reply_body"`     // the lead's actual reply text
	Category     string     `json:"reply_category"` // interested|not_interested|ooo|unsubscribe|other|""
}

// SetReplyCategory stores the AI classification of a reply.
func (s *Store) SetReplyCategory(ctx context.Context, id int64, category string) error {
	_, err := s.pool.Exec(ctx, `UPDATE messages SET reply_category=$2 WHERE id=$1`, id, category)
	return err
}

// SetReplyBody stores the lead's parsed reply text on a message (idempotent).
func (s *Store) SetReplyBody(ctx context.Context, id int64, body string) error {
	_, err := s.pool.Exec(ctx, `UPDATE messages SET reply_body=$2 WHERE id=$1`, id, body)
	return err
}

// ListReplies returns campaign messages that received a reply (warmup excluded:
// warmup mail is never stored in the messages table).
func (s *Store) ListReplies(ctx context.Context, userID int64) ([]ReplyThread, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.id, m.account_id, m.message_id, m.subject, m.body, m.sent_at,
		       (SELECT max(created_at) FROM events e WHERE e.message_id=m.id AND e.type='reply') AS replied_at,
		       l.email, trim(l.first_name || ' ' || l.last_name), c.id, c.name,
		       COALESCE((SELECT metadata->>'subject' FROM events e
		                 WHERE e.message_id=m.id AND e.type='reply' ORDER BY id DESC LIMIT 1), ''),
		       m.reply_body, m.reply_category
		FROM messages m
		JOIN campaign_leads cl ON cl.id = m.campaign_lead_id
		JOIN leads l ON l.id = cl.lead_id
		JOIN campaigns c ON c.id = cl.campaign_id
		WHERE c.user_id=$1 AND m.status='replied'
		ORDER BY replied_at DESC NULLS LAST`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReplyThread
	for rows.Next() {
		var t ReplyThread
		if err := rows.Scan(&t.MessageID, &t.AccountID, &t.RFCMessageID, &t.Subject, &t.Body,
			&t.SentAt, &t.RepliedAt, &t.LeadEmail, &t.LeadName, &t.CampaignID, &t.CampaignName,
			&t.ReplySnippet, &t.ReplyBody, &t.Category); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetReplyContext loads what's needed to send a manual reply to a thread, scoped
// to the user.
func (s *Store) GetReplyContext(ctx context.Context, userID, messageID int64) (Message, EmailAccount, Lead, bool, error) {
	m, err := s.GetMessage(ctx, messageID)
	if err != nil || m.AccountID == nil {
		return Message{}, EmailAccount{}, Lead{}, false, nil
	}
	acc, err := s.GetAccount(ctx, userID, *m.AccountID)
	if err != nil {
		return Message{}, EmailAccount{}, Lead{}, false, nil
	}
	cl, err := s.GetCampaignLead(ctx, m.CampaignLeadID)
	if err != nil {
		return Message{}, EmailAccount{}, Lead{}, false, nil
	}
	lead, err := s.GetLead(ctx, cl.LeadID)
	if err != nil {
		return Message{}, EmailAccount{}, Lead{}, false, nil
	}
	return m, acc, lead, true, nil
}

// SentMessage is one delivered campaign email, for the sent-mail view.
type SentMessage struct {
	ID           int64      `json:"id"`
	Subject      string     `json:"subject"`
	Body         string     `json:"body"`
	Status       string     `json:"status"` // sent|replied|bounced
	SentAt       *time.Time `json:"sent_at"`
	TemplateUsed string     `json:"template_used"`
	LeadEmail    string     `json:"lead_email"`
	LeadName     string     `json:"lead_name"`
	CampaignName string     `json:"campaign_name"`
	AccountEmail string     `json:"account_email"` // sending inbox
}

// ListSentMessages returns the user's delivered campaign emails, newest first.
// Warmup mail is excluded by construction (it never enters the messages table).
func (s *Store) ListSentMessages(ctx context.Context, userID int64, limit int) ([]SentMessage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.id, m.subject, m.body, m.status, m.sent_at, m.template_used,
		       l.email, trim(l.first_name || ' ' || l.last_name), c.name,
		       COALESCE(a.email, '')
		FROM messages m
		JOIN campaign_leads cl ON cl.id = m.campaign_lead_id
		JOIN leads l ON l.id = cl.lead_id
		JOIN campaigns c ON c.id = cl.campaign_id
		LEFT JOIN email_accounts a ON a.id = m.account_id
		WHERE c.user_id=$1 AND m.status IN ('sent','replied','bounced')
		ORDER BY m.sent_at DESC NULLS LAST
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SentMessage
	for rows.Next() {
		var m SentMessage
		if err := rows.Scan(&m.ID, &m.Subject, &m.Body, &m.Status, &m.SentAt, &m.TemplateUsed,
			&m.LeadEmail, &m.LeadName, &m.CampaignName, &m.AccountEmail); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
