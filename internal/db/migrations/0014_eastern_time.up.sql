-- App-wide day boundaries are Eastern Time now; move campaigns still on the
-- old UTC default to America/New_York so send windows read as ET.
UPDATE campaigns SET timezone='America/New_York' WHERE timezone='UTC';
ALTER TABLE campaigns ALTER COLUMN timezone SET DEFAULT 'America/New_York';
