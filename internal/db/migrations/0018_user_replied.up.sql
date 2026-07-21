-- Tracks when the user manually replied in a thread (via the Inbox). Once set,
-- every subsequent inbound reply on the thread triggers a notification email,
-- so an active conversation (e.g. booking a call) is never missed.
ALTER TABLE messages ADD COLUMN user_replied_at TIMESTAMPTZ;
