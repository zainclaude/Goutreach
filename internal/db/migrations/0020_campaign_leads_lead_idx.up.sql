-- campaign_leads.lead_id had no index, so the per-lead contacted/enrolled
-- EXISTS checks (and the Leads-page header counts) seq-scanned campaign_leads
-- once per lead — ~50s at 66k leads.
CREATE INDEX IF NOT EXISTS idx_campaign_leads_lead ON campaign_leads (lead_id);
