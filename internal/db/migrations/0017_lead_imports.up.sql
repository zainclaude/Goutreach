-- Track each CSV upload so the Leads page can show an import history and the
-- source file every lead came from.
CREATE TABLE lead_imports (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    filename    TEXT NOT NULL,
    imported    INT NOT NULL DEFAULT 0,
    updated     INT NOT NULL DEFAULT 0,
    skipped     INT NOT NULL DEFAULT 0,
    blacklisted INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The file a lead first arrived in (NULL for manually added / pre-feature leads).
ALTER TABLE leads ADD COLUMN IF NOT EXISTS import_id BIGINT REFERENCES lead_imports(id) ON DELETE SET NULL;
