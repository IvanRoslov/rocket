-- Empty list preserves unrestricted worker profile selection for existing runs.
ALTER TABLE creative_runs ADD COLUMN allowed_profiles TEXT NOT NULL DEFAULT '[]';
