-- Repair IMAP host for provider-backed mailboxes. A Google mailbox must read
-- from imap.gmail.com (and Outlook/365 from outlook.office365.com) regardless of
-- its domain; a custom/wrong imap_host makes the reply poller drop with
-- "unexpected EOF". Inferred from the (working) SMTP host.
UPDATE email_accounts SET imap_host='imap.gmail.com', imap_port=993
WHERE (lower(smtp_host) LIKE '%gmail.com%' OR lower(smtp_host) LIKE '%googlemail.com%')
  AND lower(imap_host) <> 'imap.gmail.com';

UPDATE email_accounts SET imap_host='outlook.office365.com', imap_port=993
WHERE (lower(smtp_host) LIKE '%outlook.com%' OR lower(smtp_host) LIKE '%office365.com%')
  AND lower(imap_host) <> 'outlook.office365.com';
