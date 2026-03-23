-- v0.9.9: Cross-channel session continuity consent.
-- When a session created on one channel (e.g., Telegram) is accessed from
-- another channel (e.g., web API), require explicit consent before allowing
-- cross-channel access. Default false = deny cross-channel access.
ALTER TABLE sessions ADD COLUMN cross_channel_consent INTEGER NOT NULL DEFAULT 0;
