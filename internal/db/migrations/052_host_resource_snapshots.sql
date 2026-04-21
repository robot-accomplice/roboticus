ALTER TABLE baseline_runs ADD COLUMN start_resources_json TEXT;
ALTER TABLE baseline_runs ADD COLUMN end_resources_json TEXT;

ALTER TABLE exercise_results ADD COLUMN resource_start_json TEXT;
ALTER TABLE exercise_results ADD COLUMN resource_end_json TEXT;

ALTER TABLE turn_diagnostics ADD COLUMN resource_snapshot_json TEXT;
