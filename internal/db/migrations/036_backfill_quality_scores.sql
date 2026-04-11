-- Backfill quality_score for existing inference_costs rows where the score was
-- never populated (always 0.0 due to empty CostMetadata). Uses the same formula
-- as llm.qualityFromResponse: min(1.0, tokens_out / 100.0).
UPDATE inference_costs
SET quality_score = MIN(1.0, CAST(tokens_out AS REAL) / 100.0)
WHERE quality_score = 0 AND tokens_out > 0;
