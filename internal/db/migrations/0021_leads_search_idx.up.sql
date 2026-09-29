-- Substring search on the Leads page does ILIKE over five columns; at 66k
-- leads the full-table scan took seconds. A trigram GIN index over the
-- concatenated searchable text serves it — the query filters on this exact
-- expression so the planner can use it.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX IF NOT EXISTS idx_leads_search ON leads
    USING gin ((email || ' ' || first_name || ' ' || last_name || ' ' || company || ' ' || title) gin_trgm_ops);
