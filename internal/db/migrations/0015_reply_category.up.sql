-- AI classification of inbound replies: interested | not_interested | ooo |
-- unsubscribe | other. Empty = not classified (pre-feature replies, AI errors).
ALTER TABLE messages ADD COLUMN IF NOT EXISTS reply_category TEXT NOT NULL DEFAULT '';
