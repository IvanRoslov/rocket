-- Templates are a global catalog. Drop only the outgoing project association;
-- retain template IDs/sequence and all incoming run references and snapshots.
ALTER TABLE creative_pipelines DROP COLUMN project_id;
