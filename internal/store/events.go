package store

import (
	"context"
	"encoding/json"
	"time"
)

// CreateEvent records an engagement event (open/click/reply/bounce).
func (s *Store) CreateEvent(ctx context.Context, messageID int64, typ string, metadata map[string]any) error {
	md := json.RawMessage(`{}`)
	if metadata != nil {
		if b, err := json.Marshal(metadata); err == nil {
			md = b
		}
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO events (message_id, type, metadata) VALUES ($1,$2,$3)`,
		messageID, typ, md)
	return err
}

// HasEvent reports whether an event of a type already exists for a message
// (used to de-duplicate opens).
func (s *Store) HasEvent(ctx context.Context, messageID int64, typ string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM events WHERE message_id=$1 AND type=$2)`,
		messageID, typ).Scan(&exists)
	return exists, err
}

// CampaignStats is the aggregate metric set for a campaign.
type CampaignStats struct {
	CampaignID int64 `json:"campaign_id"`
	Sent       int   `json:"sent"`
	Opens      int   `json:"opens"`
	Clicks     int   `json:"clicks"`
	Replies    int   `json:"replies"`
	Bounces    int   `json:"bounces"`
}

// CampaignStatsFor aggregates metrics for a single campaign.
func (s *Store) CampaignStatsFor(ctx context.Context, campaignID int64) (CampaignStats, error) {
	cs := CampaignStats{CampaignID: campaignID}
	// Sent count.
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM messages m
		 JOIN campaign_leads cl ON cl.id = m.campaign_lead_id
		 WHERE cl.campaign_id=$1 AND m.status IN ('sent','replied','bounced')`,
		campaignID).Scan(&cs.Sent)
	if err != nil {
		return cs, err
	}
	// Distinct messages per event type.
	rows, err := s.pool.Query(ctx,
		`SELECT e.type, count(DISTINCT e.message_id)
		 FROM events e
		 JOIN messages m ON m.id = e.message_id
		 JOIN campaign_leads cl ON cl.id = m.campaign_lead_id
		 WHERE cl.campaign_id=$1
		 GROUP BY e.type`, campaignID)
	if err != nil {
		return cs, err
	}
	defer rows.Close()
	for rows.Next() {
		var typ string
		var n int
		if err := rows.Scan(&typ, &n); err != nil {
			return cs, err
		}
		switch typ {
		case "open":
			cs.Opens = n
		case "click":
			cs.Clicks = n
		case "reply":
			cs.Replies = n
		case "bounce":
			cs.Bounces = n
		}
	}
	return cs, rows.Err()
}

// OverviewStats aggregates totals across all of a user's campaigns. A non-nil
// since restricts to messages sent (and events recorded) at/after that time.
func (s *Store) OverviewStats(ctx context.Context, userID int64, since *time.Time) (CampaignStats, error) {
	cs := CampaignStats{}
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM messages m
		 JOIN campaign_leads cl ON cl.id = m.campaign_lead_id
		 JOIN campaigns c ON c.id = cl.campaign_id
		 WHERE c.user_id=$1 AND m.status IN ('sent','replied','bounced')
		   AND ($2::timestamptz IS NULL OR m.sent_at >= $2)`,
		userID, since).Scan(&cs.Sent)
	if err != nil {
		return cs, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT e.type, count(DISTINCT e.message_id)
		 FROM events e
		 JOIN messages m ON m.id = e.message_id
		 JOIN campaign_leads cl ON cl.id = m.campaign_lead_id
		 JOIN campaigns c ON c.id = cl.campaign_id
		 WHERE c.user_id=$1
		   AND ($2::timestamptz IS NULL OR e.created_at >= $2)
		 GROUP BY e.type`, userID, since)
	if err != nil {
		return cs, err
	}
	defer rows.Close()
	for rows.Next() {
		var typ string
		var n int
		if err := rows.Scan(&typ, &n); err != nil {
			return cs, err
		}
		switch typ {
		case "open":
			cs.Opens = n
		case "click":
			cs.Clicks = n
		case "reply":
			cs.Replies = n
		case "bounce":
			cs.Bounces = n
		}
	}
	return cs, rows.Err()
}
