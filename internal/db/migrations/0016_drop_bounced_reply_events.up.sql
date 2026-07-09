-- Backfill: remove reply events on messages whose final status is bounced —
-- those "replies" were security-gateway rejection notices, not humans, and
-- they inflate the dashboard reply count relative to the inbox.
DELETE FROM events e USING messages m
WHERE e.message_id = m.id AND e.type = 'reply' AND m.status = 'bounced';
