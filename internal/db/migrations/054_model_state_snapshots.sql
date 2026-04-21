ALTER TABLE baseline_runs ADD COLUMN start_model_states_json TEXT;
ALTER TABLE baseline_runs ADD COLUMN end_model_states_json TEXT;

ALTER TABLE exercise_results ADD COLUMN model_state_start_json TEXT;
ALTER TABLE exercise_results ADD COLUMN model_state_end_json TEXT;
