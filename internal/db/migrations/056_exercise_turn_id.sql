ALTER TABLE exercise_results ADD COLUMN turn_id TEXT;
CREATE INDEX IF NOT EXISTS idx_exercise_results_turn ON exercise_results(turn_id);
