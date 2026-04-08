# Profile: P14 — Database Schema and SQL

## Status: PROFILED (Wave 1)

## Files Covered
- roboticus-db/src/schema.rs (1888 lines) — 25+ tables
- roboticus-db/src/sessions.rs — session CRUD
- roboticus-db/src/memory.rs — 5-tier memory
- roboticus-db/src/cron.rs — cron jobs + leases
- roboticus-db/src/metrics.rs — cost tracking
- roboticus-db/src/traces.rs — pipeline traces
- roboticus-db/src/embeddings.rs — vector storage
- roboticus-db/src/tools.rs — tool call records
- roboticus-db/src/agents.rs — sub-agent registry
- roboticus-db/src/approvals.rs — approval tracking
- roboticus-db/src/delegation.rs — delegation records
- roboticus-db/src/delivery_queue.rs — delivery persistence
- roboticus-db/src/checkpoint.rs — session checkpoints
- roboticus-db/src/policy.rs — policy decisions

## Key Tables (25+)
sessions, session_messages, turns, tool_calls, policy_decisions,
working_memory, episodic_memory, semantic_memory, procedural_memory, relationship_memory,
memory_fts (FTS5), cron_jobs, cron_runs, inference_costs, transactions,
delivery_queue, approval_requests, embeddings, sub_agents, context_checkpoints,
pipeline_traces, delegation_outcomes, tasks, skills

## Critical Behaviors
1. Session scope_key: "agent" | "peer:{channel}:{peer_id}" | "group:{channel}:{group_id}"
2. Unique active session: idx_sessions_active_scope_unique enforces one active per (agent_id, scope_key)
3. Cron lease: 60-second atomic UPDATE with WHERE lease_holder IS NULL OR lease_expires_at < datetime('now')
4. Embedding BLOB: 4-byte little-endian IEEE 754 floats
5. FTS5: default porter stemmer tokenizer, spans working/episodic/semantic tiers
6. Semantic memory: UNIQUE(category, key) with ON CONFLICT DO UPDATE resets memory_state='active'
7. Hybrid search: FTS score * (1-weight) + vector score * weight, merged and sorted
8. Cascading cron delete: dynamic FK resolution via sqlite_master + PRAGMA foreign_key_list
9. Delegation stats: json_each() unpacks assigned_agents_json for per-agent success rates

## Inference Costs Columns
id, model, provider, tokens_in, tokens_out, cost, tier, cached, latency_ms, quality_score, escalation, turn_id, created_at

Full behavioral detail including exact SQL statements in agent session context.
