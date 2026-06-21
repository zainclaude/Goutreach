-- OAuth support for connecting mailboxes (e.g. "Sign in with Google").
-- auth_type: 'password' (SMTP/IMAP password or app password) or 'oauth' (XOAUTH2).
ALTER TABLE email_accounts ADD COLUMN auth_type TEXT NOT NULL DEFAULT 'password';
-- Encrypted OAuth refresh token (used to mint short-lived access tokens for XOAUTH2).
ALTER TABLE email_accounts ADD COLUMN oauth_refresh_token_enc TEXT NOT NULL DEFAULT '';
