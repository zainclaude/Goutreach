package store

import (
	"context"
	"strings"
	"time"
)

// Message is a generated/sent email.
type Message struct {
	ID             int64      `json:"id"`
	CampaignLeadID int64      `json:"campaign_lead_id"`
	StepIndex      int        `json:"step_index"`
	AccountID      *int64     `json:"account_id"`
	Subject        string     `json:"subject"`
	Body           string     `json:"body"`
	MessageID      string     `json:"message_id"`
	InReplyTo      string     `json:"in_reply_to"`
	References     string     `json:"references_hdr"`
	Status         string     `json:"status"`
	Error          string     `json:"error"`
	TemplateUsed   string     `json:"template_used"`
	ResearchNotes  string     `json:"research_notes"`
	Approved       bool       `json:"approved"`
	SentAt         *time.Time `json:"sent_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

const messageCols = `id, campaign_lead_id, step_index, account_id, subject, body, message_id,
	in_reply_to, references_hdr, status, error, template_used, research_notes, approved, sent_at, created_at`

func scanMessage(row interface{ Scan(...any) error }) (Message, error) {
	var m Message
	err := row.Scan(&m.ID, &m.CampaignLeadID, &m.StepIndex, &m.AccountID, &m.Subject, &m.Body,
		&m.MessageID, &m.InReplyTo, &m.References, &m.Status, &m.Error,
		&m.TemplateUsed, &m.ResearchNotes, &m.Approved, &m.SentAt, &m.CreatedAt)
	return m, err
}

// CreateMessage inserts a queued message row.
func (s *Store) CreateMessage(ctx context.Context, m Message) (Message, error) {
	row := s.pool.QueryRow(ctx,
		`INSERT INTO messages (campaign_lead_id, step_index, status, approved)
		 VALUES ($1,$2,$3,$4) RETURNING `+messageCols,
		m.CampaignLeadID, m.StepIndex, statusOr(m.Status, "queued"), m.Approved)
	return scanMessage(row)
}

// GetMessageForStep returns the latest message for a (campaign_lead, step), if any.
func (s *Store) GetMessageForStep(ctx context.Context, campaignLeadID int64, step int) (Message, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+messageCols+` FROM messages
		 WHERE campaign_lead_id=$1 AND step_index=$2 ORDER BY id DESC LIMIT 1`,
		campaignLeadID, step)
	return scanMessage(row)
}

// SetMessageGenerated stores the AI-generated subject/body and research metadata.
func (s *Store) SetMessageGenerated(ctx context.Context, id int64, subject, body, templateUsed, researchNotes string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE messages SET subject=$2, body=$3, template_used=$4, research_notes=$5,
		   status='generated', error='' WHERE id=$1`,
		id, subject, body, templateUsed, researchNotes)
	return err
}

// SetMessageApproved flips the approval flag for a message.
func (s *Store) SetMessageApproved(ctx context.Context, id int64, approved bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE messages SET approved=$2 WHERE id=$1`, id, approved)
	return err
}

// ListMessagesForCampaign returns messages (with lead email) for review/preview.
func (s *Store) ListMessagesForCampaign(ctx context.Context, userID, campaignID int64, limit int) ([]MessageWithLead, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+prefixCols("m", messageCols)+`, l.email, l.company
		 FROM messages m
		 JOIN campaign_leads cl ON cl.id = m.campaign_lead_id
		 JOIN campaigns c ON c.id = cl.campaign_id
		 JOIN leads l ON l.id = cl.lead_id
		 WHERE c.id=$1 AND c.user_id=$2
		 ORDER BY m.id DESC LIMIT $3`, campaignID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MessageWithLead
	for rows.Next() {
		var mw MessageWithLead
		err := rows.Scan(&mw.ID, &mw.CampaignLeadID, &mw.StepIndex, &mw.AccountID, &mw.Subject,
			&mw.Body, &mw.MessageID, &mw.InReplyTo, &mw.References, &mw.Status, &mw.Error,
			&mw.TemplateUsed, &mw.ResearchNotes, &mw.Approved, &mw.SentAt, &mw.CreatedAt,
			&mw.LeadEmail, &mw.LeadCompany)
		if err != nil {
			return nil, err
		}
		out = append(out, mw)
	}
	return out, rows.Err()
}

// MessageWithLead augments a message with lead identity for review UIs.
type MessageWithLead struct {
	Message
	LeadEmail   string `json:"lead_email"`
	LeadCompany string `json:"lead_company"`
}

// SetMessageSent marks a message sent with its threading headers.
func (s *Store) SetMessageSent(ctx context.Context, id, accountID int64, messageID, inReplyTo, references string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE messages SET status='sent', account_id=$2, message_id=$3, in_reply_to=$4,
		   references_hdr=$5, sent_at=now() WHERE id=$1`,
		id, accountID, messageID, inReplyTo, references)
	return err
}

// SetMessageFailed records a send/generation failure.
func (s *Store) SetMessageFailed(ctx context.Context, id int64, errMsg string) error {
	_, err := s.pool.Exec(ctx, `UPDATE messages SET status='failed', error=$2 WHERE id=$1`, id, errMsg)
	return err
}

// UpdateMessageContent edits a not-yet-sent message's subject/body.
func (s *Store) UpdateMessageContent(ctx context.Context, id int64, subject, body string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE messages SET subject=$2, body=$3 WHERE id=$1 AND status NOT IN ('sent','replied','bounced')`,
		id, subject, body)
	return err
}

// DeleteMessage removes a message (used to reject a preview).
func (s *Store) DeleteMessage(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM messages WHERE id=$1 AND status NOT IN ('sent','replied')`, id)
	return err
}

// SetMessageStatus updates only the status (e.g. replied, bounced).
func (s *Store) SetMessageStatus(ctx context.Context, id int64, status string) error {
	_, err := s.pool.Exec(ctx, `UPDATE messages SET status=$2 WHERE id=$1`, id, status)
	return err
}

// GetMessage returns one message by id.
func (s *Store) GetMessage(ctx context.Context, id int64) (Message, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+messageCols+` FROM messages WHERE id=$1`, id)
	return scanMessage(row)
}

// FindSentMessageByThread matches an inbound reply to a previously-sent message.
// It checks our Message-ID against the inbound In-Reply-To / References values.
func (s *Store) FindSentMessageByThread(ctx context.Context, accountID int64, candidates []string) (Message, bool, error) {
	for _, c := range candidates {
		if c == "" {
			continue
		}
		row := s.pool.QueryRow(ctx,
			`SELECT `+messageCols+` FROM messages
			 WHERE account_id=$1 AND message_id=$2 AND status IN ('sent','replied') LIMIT 1`,
			accountID, c)
		m, err := scanMessage(row)
		if err == nil {
			return m, true, nil
		}
	}
	return Message{}, false, nil
}

// LatestSentToRecipient finds the most recent sent message from an account to a
// given recipient email (fallback reply matching when threading headers are absent).
func (s *Store) LatestSentToRecipient(ctx context.Context, accountID int64, recipient string) (Message, bool, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+messageCols+` FROM messages m
		 JOIN campaign_leads cl ON cl.id = m.campaign_lead_id
		 JOIN leads l ON l.id = cl.lead_id
		 WHERE m.account_id=$1 AND lower(l.email)=lower($2) AND m.status IN ('sent','replied')
		 ORDER BY m.sent_at DESC LIMIT 1`,
		accountID, recipient)
	m, err := scanMessage(row)
	if err != nil {
		return Message{}, false, nil
	}
	return m, true, nil
}

func statusOr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// prefixCols rewrites a comma-separated column list to alias each column, e.g.
// prefixCols("m", "id, body") => "m.id, m.body".
func prefixCols(alias, cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = alias + "." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}
