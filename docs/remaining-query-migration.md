# Remaining Generic Query/QueryRow → Named Method Migrations

## Status — COMPLETE
- 104 of 105 route query calls converted to named methods
- 1 intentionally generic: `SearchTraces` dynamic query (user-driven filters)
- 0 raw `store.ExecContext()` calls in production route handlers
- 23/23 packages pass, all fitness/architecture tests pass

## Pattern
For each call:
1. Read the SQL from the route file
2. Create a named method in `internal/db/route_queries.go` with exact column/order match
3. Replace the generic call with the named method
4. Verify build + tests pass

## CRITICAL: Column order must match exactly
Named methods must return columns in the same order the route's `rows.Scan()` expects.
Several earlier replacements broke because column order didn't match. Always check the Scan call.

## Files

### session_detail.go (6 calls)
- L94: `SELECT COUNT(*) FROM session_messages WHERE session_id = ?` → `SessionMessageCount(sessionID)`
- L101: `SELECT COUNT(*) FROM tool_calls tc JOIN turns t...` → `SessionToolCallCount(sessionID)` (already exists)
- L421-445: 4x skill count queries → `CountEnabledSkills()`, `CountDisabledSkills()`, `CountAllSkills()`, `LatestSkillTimestamp()`

### memory_analytics.go (10 calls)
- L20: retrieval quality average → `RetrievalQualityAvg(hours)`
- L38: cache performance → `CachePerformance(hours)`
- L52: complexity distribution → `ComplexityDistribution(hours)`
- L75: memory utilization → `MemoryUtilization()`
- L86: memory trend → `MemoryTrend(hours)`
- L104-124: 5x tier counts → `CountWorkingMemory()`, `CountEpisodicMemory()`, `CountSemanticMemory()`, `CountProceduralMemory()`, `CountRelationshipMemory()`

### stats.go (9 calls)
- L18: cost timeseries → `CostsByHour(hours)` (already exists)
- L56: total cost → `TotalCostSince(hours)` (already exists)
- L66: cost by model → `CostsByModel(hours)` (already exists)
- L84: cache stats → `CacheStats()`
- L166: efficiency metrics → `EfficiencyMetrics(period)`
- L186: model cost breakdown → `ModelCostBreakdown(hours)`
- L279: transactions → `ListRecentTransactions(hours, limit)` (already exists)
- L336: token budget utilization → `TokenBudgetUtilization(hours)`
- L445: capacity metrics → `ProviderCapacity(hours)` (already exists)

### turn_detail.go (8 calls)
- L19: turn detail → `GetTurnDetail(turnID)`
- L90: turn messages → `TurnMessages(turnID)` (already exists)
- L146: turn tool calls → `TurnToolCalls(turnID)` (already exists)
- L175: turn context → `TurnContextSnapshot(turnID)` (already exists)
- L222: turn feedback → `TurnFeedbackByTurnID(turnID)`
- L234: cached flag → `TurnCachedFlag(turnID)`
- L238: tool counts → `ToolCallCountsForTurn(turnID)` (already exists)
- L245: context snapshot → `ContextSnapshotForTurn(turnID)` (already exists)

### admin.go (6 calls)
- L348-374: 5x memory health counts → reuse tier count methods from memory_analytics
- L422: subagent list → `ListSubAgents()` (already exists)

### traces.go (7 calls)
- L17: list traces → `ListPipelineTraces(limit)` (already exists)
- L77: dynamic trace query → keep as generic (user-driven filter)
- L112: trace by ID → `GetPipelineTrace(id)` (already exists)
- L140: trace react_json → `GetReactTraceForPipeline(traceID)`
- L177: turn detail from trace → `GetTurnFromTrace(turnID)`
- L207: model selection for turn → `GetModelSelectionForTurn(turnID)`
- L238: cost for turn → `GetCostForTurn(turnID)`

### observability.go (6 calls)
- L17: delegation outcomes → `ListDelegationOutcomes(limit)` (already exists)
- L45: trace count → `CountPipelineTraces()`
- L63: escalation stats → `EscalationStats(hours)`
- L91: abuse events → `ListAbuseEvents(limit)` (already exists)
- L135-144: 2x delegation stats → `DelegationSuccessRate(hours)`, `DelegationAvgDuration(hours)`

### workspace.go (5 calls)
- L25: active session count → `CountActiveSessions()` (already exists)
- L53: subagent list (simple) → `ListSubAgentsSimple()`
- L106: enabled skill count → `CountEnabledSkills()`
- L108: skill names list → `ListEnabledSkillNames(limit)`
- L132: subagent roster → `ListSubAgentRoster()`

### turns_skills.go (4 calls)
- L20: recent turns → `ListRecentTurns(limit)`
- L47: turn by ID → `GetTurnByID(turnID)`
- L101: session for turn → `GetSessionForTurn(turnID)`
- L125: skill executions → `ListSkillExecutions(limit)`

### throttle.go (4 calls)
- L18: abuse summary → `AbuseSummary(hours)`
- L28: abuse by type → `AbuseByType(hours)`
- L54: abuse by actor → `AbuseByActor(hours)`
- L82: rate limit current → `RateLimitCurrent()`

## Named methods already in route_queries.go
ListSessions, GetSession, CountActiveSessions, SessionMessages, SessionMessageCount,
ListSkillsAll, GetSkillByID, CountSkills, ListSubAgents, GetSubAgentByName,
ListTurnsBySession, ListPipelineTraces, GetPipelineTrace, ListInferenceCosts,
TotalCostSince, ListToolCallsByTurn, ListCronJobs, GetCronJob, ListCronRuns,
GetCronJobPayload, ListWorkingMemory, ListEpisodicMemory, ListSemanticMemory,
ListContextSnapshots, ListDelegationOutcomes, ListModelSelections, ListAbuseEvents,
ListTurnFeedback, ListThemes, SessionsWithoutNicknames, SessionExists,
ListTurnsForAnalysis, ContextSnapshotForTurn, ToolCallCountsForTurn,
SessionFeedbackGrades, SessionTurnsWithMessages, SessionFeedback, SessionTurnStats,
SessionToolCallCount, SemanticCategories, SearchWorkingMemory, SearchEpisodicMemory,
SearchSemanticMemory, CostsByHour, CostsByModel, CountRow, TurnMessages, TurnToolCalls,
TurnContextSnapshot, ListRecentTransactions, ProviderCapacity, ListDiscoveredAgents,
ListPairedDevices, GetRuntimeSetting, GetIdentityValue, ListPolicyDecisions,
ListToolCallsForAudit, ListWalletBalances, ListRetirementCandidates, ListDeadLetters
