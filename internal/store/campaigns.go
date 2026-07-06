package store

import (
	"context"
	"errors"
	"time"
)

// errNotFound is returned when a scoped update affects no rows.
var errNotFound = errors.New("not found")

// Campaign is an outreach effort.
type Campaign struct {
	ID              int64     `json:"id"`
	UserID          int64     `json:"user_id"`
	Name            string    `json:"name"`
	Brief           string    `json:"brief"`
	Status          string    `json:"status"`
	Timezone        string    `json:"timezone"`
	SendStartHour   int       `json:"send_start_hour"`
	SendEndHour     int       `json:"send_end_hour"`
	SendWeekdays    []int32   `json:"send_weekdays"`
	DailyCap        int       `json:"daily_cap"`
	TrackOpens      bool      `json:"track_opens"`
	TrackClicks     bool      `json:"track_clicks"`
	RequireApproval bool      `json:"require_approval"`
	ApprovalCount   int       `json:"approval_count"`
	CreatedAt       time.Time `json:"created_at"`
}

// CampaignStep is one step in a sequence.
type CampaignStep struct {
	ID         int64  `json:"id"`
	CampaignID int64  `json:"campaign_id"`
	StepIndex  int    `json:"step_index"`
	DelayDays  int    `json:"delay_days"`
	Angle      string `json:"angle"`
}

// CampaignLead is a lead enrolled in a campaign.
type CampaignLead struct {
	ID          int64     `json:"id"`
	CampaignID  int64     `json:"campaign_id"`
	LeadID      int64     `json:"lead_id"`
	CurrentStep int       `json:"current_step"`
	Status      string    `json:"status"`
	NextSendAt  time.Time `json:"next_send_at"`
	CreatedAt   time.Time `json:"created_at"`
}

const campaignCols = `id, user_id, name, brief, status, timezone, send_start_hour,
	send_end_hour, send_weekdays, daily_cap, track_opens, track_clicks,
	require_approval, approval_count, created_at`

func scanCampaign(row interface{ Scan(...any) error }) (Campaign, error) {
	var c Campaign
	err := row.Scan(&c.ID, &c.UserID, &c.Name, &c.Brief, &c.Status, &c.Timezone,
		&c.SendStartHour, &c.SendEndHour, &c.SendWeekdays, &c.DailyCap,
		&c.TrackOpens, &c.TrackClicks, &c.RequireApproval, &c.ApprovalCount, &c.CreatedAt)
	return c, err
}

// CreateCampaign inserts a campaign.
func (s *Store) CreateCampaign(ctx context.Context, c Campaign) (Campaign, error) {
	if len(c.SendWeekdays) == 0 {
		c.SendWeekdays = []int32{1, 2, 3, 4, 5}
	}
	if c.ApprovalCount == 0 {
		c.ApprovalCount = 5
	}
	row := s.pool.QueryRow(ctx,
		`INSERT INTO campaigns (user_id, name, brief, timezone, send_start_hour, send_end_hour,
		   send_weekdays, daily_cap, track_opens, track_clicks, require_approval, approval_count)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		 RETURNING `+campaignCols,
		c.UserID, c.Name, c.Brief, c.Timezone, c.SendStartHour, c.SendEndHour,
		c.SendWeekdays, c.DailyCap, c.TrackOpens, c.TrackClicks, c.RequireApproval, c.ApprovalCount)
	return scanCampaign(row)
}

// ListCampaigns returns campaigns for a user.
func (s *Store) ListCampaigns(ctx context.Context, userID int64) ([]Campaign, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+campaignCols+` FROM campaigns WHERE user_id=$1 ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Campaign
	for rows.Next() {
		c, err := scanCampaign(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCampaign returns a single campaign scoped to a user.
func (s *Store) GetCampaign(ctx context.Context, userID, id int64) (Campaign, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+campaignCols+` FROM campaigns WHERE id=$1 AND user_id=$2`, id, userID)
	return scanCampaign(row)
}

// GetCampaignByID returns a campaign without user scope (for workers).
func (s *Store) GetCampaignByID(ctx context.Context, id int64) (Campaign, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+campaignCols+` FROM campaigns WHERE id=$1`, id)
	return scanCampaign(row)
}

// SetCampaignStatus updates a campaign's status.
func (s *Store) SetCampaignStatus(ctx context.Context, userID, id int64, status string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE campaigns SET status=$3 WHERE id=$1 AND user_id=$2`, id, userID, status)
	return err
}

// CompleteFinishedCampaigns marks every running campaign "completed" once all of
// its enrolled leads have finished (none still 'active'). Campaigns with no leads
// at all are left running. Returns how many transitioned. Run as a periodic sweep
// so campaigns that finished before being checked still get completed.
func (s *Store) CompleteFinishedCampaigns(ctx context.Context) (int, error) {
	ct, err := s.pool.Exec(ctx, `
		UPDATE campaigns c SET status='completed'
		WHERE c.status='running'
		  AND EXISTS (SELECT 1 FROM campaign_leads cl WHERE cl.campaign_id=c.id)
		  AND NOT EXISTS (SELECT 1 FROM campaign_leads cl WHERE cl.campaign_id=c.id AND cl.status='active')`)
	if err != nil {
		return 0, err
	}
	return int(ct.RowsAffected()), nil
}

// ReactivateIfCompleted flips a completed campaign back to running, e.g. after
// new leads are enrolled. No-op for any other status.
func (s *Store) ReactivateIfCompleted(ctx context.Context, userID, id int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE campaigns SET status='running' WHERE id=$1 AND user_id=$2 AND status='completed'`,
		id, userID)
	return err
}

// ReplaceSteps deletes and re-inserts the steps for a campaign.
func (s *Store) ReplaceSteps(ctx context.Context, campaignID int64, steps []CampaignStep) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM campaign_steps WHERE campaign_id=$1`, campaignID); err != nil {
		return err
	}
	for _, st := range steps {
		if _, err := tx.Exec(ctx,
			`INSERT INTO campaign_steps (campaign_id, step_index, delay_days, angle)
			 VALUES ($1,$2,$3,$4)`,
			campaignID, st.StepIndex, st.DelayDays, st.Angle); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ListSteps returns ordered steps for a campaign.
func (s *Store) ListSteps(ctx context.Context, campaignID int64) ([]CampaignStep, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, campaign_id, step_index, delay_days, angle
		 FROM campaign_steps WHERE campaign_id=$1 ORDER BY step_index`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CampaignStep
	for rows.Next() {
		var st CampaignStep
		if err := rows.Scan(&st.ID, &st.CampaignID, &st.StepIndex, &st.DelayDays, &st.Angle); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// GetStep returns one step by campaign + index.
func (s *Store) GetStep(ctx context.Context, campaignID int64, stepIndex int) (CampaignStep, error) {
	var st CampaignStep
	err := s.pool.QueryRow(ctx,
		`SELECT id, campaign_id, step_index, delay_days, angle
		 FROM campaign_steps WHERE campaign_id=$1 AND step_index=$2`, campaignID, stepIndex,
	).Scan(&st.ID, &st.CampaignID, &st.StepIndex, &st.DelayDays, &st.Angle)
	return st, err
}

// SetCampaignAccounts replaces the inbox pool for a campaign.
func (s *Store) SetCampaignAccounts(ctx context.Context, campaignID int64, accountIDs []int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM campaign_accounts WHERE campaign_id=$1`, campaignID); err != nil {
		return err
	}
	for _, aid := range accountIDs {
		if _, err := tx.Exec(ctx,
			`INSERT INTO campaign_accounts (campaign_id, account_id) VALUES ($1,$2)
			 ON CONFLICT DO NOTHING`, campaignID, aid); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ListCampaignAccountIDs returns the inbox pool for a campaign.
func (s *Store) ListCampaignAccountIDs(ctx context.Context, campaignID int64) ([]int64, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT account_id FROM campaign_accounts WHERE campaign_id=$1`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// EnrollLeads adds leads to a campaign as active, due immediately for step 0.
func (s *Store) EnrollLeads(ctx context.Context, campaignID int64, leadIDs []int64) (int, error) {
	added := 0
	for _, lid := range leadIDs {
		// The join refuses leads whose email verification came back invalid or
		// risky — they can't be enrolled through any path (picker or bulk).
		ct, err := s.pool.Exec(ctx,
			`INSERT INTO campaign_leads (campaign_id, lead_id, next_send_at)
			 SELECT $1, l.id, now() FROM leads l
			 WHERE l.id = $2 AND l.verification_status NOT IN ('invalid','risky')
			 ON CONFLICT DO NOTHING`, campaignID, lid)
		if err != nil {
			return added, err
		}
		added += int(ct.RowsAffected())
	}
	return added, nil
}

// CampaignLeadDetail is an enrolled lead with its enrollment state, for display.
type CampaignLeadDetail struct {
	LeadID       int64      `json:"lead_id"`
	Email        string     `json:"email"`
	FirstName    string     `json:"first_name"`
	LastName     string     `json:"last_name"`
	Company      string     `json:"company"`
	Status       string     `json:"status"`
	CurrentStep  int        `json:"current_step"`
	Contacted    bool       `json:"contacted"`           // emailed before (in any campaign)
	Verification string     `json:"verification_status"` // unknown|valid|invalid|risky|catch_all
	VerifiedAt   *time.Time `json:"verified_at"`         // nil = never checked
}

// ListCampaignLeadDetails returns the leads enrolled in a campaign with their
// enrollment status and current step.
func (s *Store) ListCampaignLeadDetails(ctx context.Context, campaignID int64) ([]CampaignLeadDetail, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT l.id, l.email, l.first_name, l.last_name, l.company, cl.status, cl.current_step,
		       EXISTS(SELECT 1 FROM campaign_leads cl2 JOIN messages m ON m.campaign_lead_id=cl2.id
		              WHERE cl2.lead_id = l.id AND m.status IN ('sent','replied','bounced')) AS contacted,
		       l.verification_status, l.verified_at
		FROM campaign_leads cl JOIN leads l ON l.id = cl.lead_id
		WHERE cl.campaign_id=$1
		ORDER BY l.id DESC`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CampaignLeadDetail
	for rows.Next() {
		var d CampaignLeadDetail
		if err := rows.Scan(&d.LeadID, &d.Email, &d.FirstName, &d.LastName, &d.Company, &d.Status, &d.CurrentStep, &d.Contacted, &d.Verification, &d.VerifiedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// UnenrollAllLeads removes every lead from a campaign, cascading to their
// generated/sent message rows. Returns how many enrollments were removed.
func (s *Store) UnenrollAllLeads(ctx context.Context, campaignID int64) (int, error) {
	ct, err := s.pool.Exec(ctx, `DELETE FROM campaign_leads WHERE campaign_id=$1`, campaignID)
	if err != nil {
		return 0, err
	}
	return int(ct.RowsAffected()), nil
}

// UnenrollLead removes a single lead from a campaign (its draft messages cascade).
func (s *Store) UnenrollLead(ctx context.Context, campaignID, leadID int64) (int, error) {
	ct, err := s.pool.Exec(ctx,
		`DELETE FROM campaign_leads WHERE campaign_id=$1 AND lead_id=$2`, campaignID, leadID)
	if err != nil {
		return 0, err
	}
	return int(ct.RowsAffected()), nil
}

// DueCampaignLeads returns active enrollments for running campaigns whose next_send_at has passed.
func (s *Store) DueCampaignLeads(ctx context.Context, limit int) ([]CampaignLead, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT cl.id, cl.campaign_id, cl.lead_id, cl.current_step, cl.status, cl.next_send_at, cl.created_at
		 FROM campaign_leads cl
		 JOIN campaigns c ON c.id = cl.campaign_id
		 WHERE cl.status='active' AND c.status='running' AND cl.next_send_at <= now()
		 ORDER BY cl.next_send_at
		 LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CampaignLead
	for rows.Next() {
		var cl CampaignLead
		if err := rows.Scan(&cl.ID, &cl.CampaignID, &cl.LeadID, &cl.CurrentStep,
			&cl.Status, &cl.NextSendAt, &cl.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, cl)
	}
	return out, rows.Err()
}

// AdvanceCampaignLead moves a lead to the next step or finishes it.
func (s *Store) AdvanceCampaignLead(ctx context.Context, id int64, nextStep int, nextSendAt time.Time, status string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE campaign_leads SET current_step=$2, next_send_at=$3, status=$4 WHERE id=$1`,
		id, nextStep, nextSendAt, status)
	return err
}

// SetCampaignLeadStatus updates only the status (e.g. replied, bounced).
func (s *Store) SetCampaignLeadStatus(ctx context.Context, id int64, status string) error {
	_, err := s.pool.Exec(ctx, `UPDATE campaign_leads SET status=$2 WHERE id=$1`, id, status)
	return err
}

// ListActiveCampaignLeads returns the first N active enrollments for a campaign.
func (s *Store) ListActiveCampaignLeads(ctx context.Context, campaignID int64, limit int) ([]CampaignLead, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, campaign_id, lead_id, current_step, status, next_send_at, created_at
		 FROM campaign_leads WHERE campaign_id=$1 AND status='active'
		 ORDER BY id LIMIT $2`, campaignID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CampaignLead
	for rows.Next() {
		var cl CampaignLead
		if err := rows.Scan(&cl.ID, &cl.CampaignID, &cl.LeadID, &cl.CurrentStep,
			&cl.Status, &cl.NextSendAt, &cl.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, cl)
	}
	return out, rows.Err()
}

// LaunchCampaign turns off the approval gate, approves all unsent generated
// messages, and sets the campaign running.
func (s *Store) LaunchCampaign(ctx context.Context, userID, id int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	ct, err := tx.Exec(ctx,
		`UPDATE campaigns SET require_approval=false, status='running' WHERE id=$1 AND user_id=$2`,
		id, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errNotFound
	}
	if _, err := tx.Exec(ctx,
		`UPDATE messages SET approved=true
		 WHERE approved=false AND status IN ('generated','queued')
		   AND campaign_lead_id IN (SELECT id FROM campaign_leads WHERE campaign_id=$1)`,
		id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// GetCampaignLead returns one enrollment.
func (s *Store) GetCampaignLead(ctx context.Context, id int64) (CampaignLead, error) {
	var cl CampaignLead
	err := s.pool.QueryRow(ctx,
		`SELECT id, campaign_id, lead_id, current_step, status, next_send_at, created_at
		 FROM campaign_leads WHERE id=$1`, id,
	).Scan(&cl.ID, &cl.CampaignID, &cl.LeadID, &cl.CurrentStep, &cl.Status, &cl.NextSendAt, &cl.CreatedAt)
	return cl, err
}

// CampaignLeadStateCounts summarizes enrollment state for the send-status
// diagnosis: how many leads are active, due now, held unverified, invalid/risky
// (will be skipped), and terminal.
type CampaignLeadStateCounts struct {
	Total      int        `json:"total"`
	Active     int        `json:"active"`
	DueNow     int        `json:"due_now"`
	Unverified int        `json:"unverified"` // active + never verified
	BadEmail   int        `json:"bad_email"`  // active + invalid/risky
	Skipped    int        `json:"skipped"`
	Finished   int        `json:"finished"`
	Replied    int        `json:"replied"`
	Bounced    int        `json:"bounced"`
	NextSendAt *time.Time `json:"next_send_at"`
}

func (s *Store) CampaignLeadStates(ctx context.Context, campaignID int64) (CampaignLeadStateCounts, error) {
	var c CampaignLeadStateCounts
	err := s.pool.QueryRow(ctx, `
		SELECT count(*),
		  count(*) FILTER (WHERE cl.status='active'),
		  count(*) FILTER (WHERE cl.status='active' AND cl.next_send_at <= now()),
		  count(*) FILTER (WHERE cl.status='active' AND l.verification_status='unknown' AND l.verified_at IS NULL),
		  count(*) FILTER (WHERE cl.status='active' AND l.verification_status IN ('invalid','risky')),
		  count(*) FILTER (WHERE cl.status='skipped'),
		  count(*) FILTER (WHERE cl.status='finished'),
		  count(*) FILTER (WHERE cl.status='replied'),
		  count(*) FILTER (WHERE cl.status='bounced'),
		  min(cl.next_send_at) FILTER (WHERE cl.status='active')
		FROM campaign_leads cl JOIN leads l ON l.id = cl.lead_id
		WHERE cl.campaign_id=$1`, campaignID).Scan(
		&c.Total, &c.Active, &c.DueNow, &c.Unverified, &c.BadEmail,
		&c.Skipped, &c.Finished, &c.Replied, &c.Bounced, &c.NextSendAt)
	return c, err
}
