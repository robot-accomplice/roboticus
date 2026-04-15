package pipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	agenttools "roboticus/internal/agent/tools"
	"roboticus/internal/core"
	"roboticus/internal/db"
	"roboticus/internal/llm"
)

// ── Stage 1: Input validation ──────────────────────────────────────────

func (p *Pipeline) stageValidation(_ context.Context, pc *pipelineContext) error {
	pc.tr.BeginSpan("validation")

	// Bot command early-exit marker: checked after session resolution (Stage 4b).
	pc.isBotCommand = pc.cfg.BotCommandDispatch && len(pc.input.Content) > 0 && pc.input.Content[0] == '/'

	// Cron delegation wrap: prepend subagent directive for non-root cron tasks.
	if pc.cfg.CronDelegationWrap && pc.input.AgentID != "" && pc.input.AgentID != "default" {
		pc.input.Content = fmt.Sprintf("[Delegated to %s] %s", pc.input.AgentID, pc.input.Content)
	}

	// API-level model override takes precedence over config.
	if pc.input.ModelOverride != "" {
		pc.cfg.ModelOverride = pc.input.ModelOverride
	}

	// Prefer local model: scan fallbacks for a local provider and set override.
	if pc.cfg.PreferLocalModel && pc.cfg.ModelOverride == "" {
		pc.cfg.ModelOverride = p.findLocalModel()
	}

	if pc.input.Content == "" {
		pc.tr.EndSpan("error")
		return core.NewError(core.ErrConfig, "empty message content")
	}
	if len(pc.input.Content) > core.MaxUserMessageBytes {
		pc.tr.EndSpan("error")
		return core.NewError(core.ErrConfig, fmt.Sprintf("message exceeds %d bytes", core.MaxUserMessageBytes))
	}
	pc.tr.Annotate("content_len", len(pc.input.Content))
	pc.tr.Annotate("channel", pc.cfg.ChannelLabel)
	pc.tr.Annotate("agent_id", pc.input.AgentID)
	if pc.isBotCommand {
		pc.tr.Annotate("bot_command_detected", true)
	}
	if pc.cfg.ModelOverride != "" {
		pc.tr.Annotate("model_override", pc.cfg.ModelOverride)
	}
	if pc.cfg.PreferLocalModel {
		pc.tr.Annotate("prefer_local", true)
	}
	pc.tr.EndSpan("ok")
	return nil
}

// ── Stage 2: Injection defense ─────────────────────────────────────────

func (p *Pipeline) stageInjectionDefense(_ context.Context, pc *pipelineContext) error {
	pc.tr.BeginSpan("injection_defense")
	if pc.cfg.InjectionDefense && p.injection != nil {
		score := p.injection.CheckInput(pc.input.Content)
		pc.tr.Annotate("score", float64(score))
		if score.IsBlocked() {
			pc.tr.Annotate("action", "blocked")
			pc.tr.EndSpan("error")
			log.Warn().Float64("score", float64(score)).Str("channel", pc.cfg.ChannelLabel).Str("session", pc.input.SessionID).Str("agent", pc.input.AgentID).Str("sender", pc.input.SenderID).Msg("injection blocked")
			return core.NewError(core.ErrInjectionBlocked, "input rejected by injection defense")
		}
		if score.IsCaution() {
			pc.input.Content = p.injection.Sanitize(pc.input.Content)
			pc.threatCaution = true
			pc.tr.Annotate("action", "sanitized")
			log.Warn().Float64("score", float64(score)).Str("session", pc.input.SessionID).Str("channel", pc.cfg.ChannelLabel).Str("agent", pc.input.AgentID).Msg("input sanitized")
		} else {
			pc.tr.Annotate("action", "pass")
		}
	}
	pc.tr.EndSpan("ok")
	return nil
}

// ── Stage 3: Dedup tracking + task creation ────────────────────────────
// The caller (Run) is responsible for deferring dedup.Release via pc.dedupFP.

func (p *Pipeline) stageDedup(_ context.Context, pc *pipelineContext) error {
	if pc.cfg.DedupTracking && p.dedup != nil {
		pc.tr.BeginSpan("dedup_check")
		pc.dedupFP = Fingerprint(pc.input.Content, pc.input.AgentID, pc.input.SessionID)
		pc.tr.Annotate("fingerprint", pc.dedupFP)
		if !p.dedup.CheckAndTrack(pc.dedupFP) {
			pc.tr.Annotate("duplicate", true)
			pc.tr.EndSpan("rejected")
			return core.NewError(core.ErrDuplicate, "duplicate request already in flight")
		}
		pc.tr.EndSpan("ok")
	}

	// Create task for lifecycle tracking.
	pc.taskID = db.NewID()
	task := p.tasks.Create(pc.taskID, pc.input.SessionID, pc.input.Content)
	_ = task
	return nil
}

// ── Stage 4: Session resolution + 4a consent + 4b bot command + short-followup ──

func (p *Pipeline) stageSessionResolution(ctx context.Context, pc *pipelineContext) (*Outcome, error) {
	pc.tr.BeginSpan("session_resolution")
	session, err := p.resolveSession(ctx, pc.cfg, pc.input)
	if err != nil {
		pc.tr.EndSpan("error")
		return nil, core.WrapError(core.ErrDatabase, "session resolution failed", err)
	}
	pc.session = session
	pc.tr.Annotate("session_id", pc.session.ID)
	pc.tr.EndSpan("ok")

	// Stage 4a: Cross-channel consent check (Rust parity).
	consentResult, consentMsg := p.checkCrossChannelConsent(ctx, pc.session, pc.input)
	switch consentResult {
	case ConsentGranted:
		return &Outcome{SessionID: pc.session.ID, Content: consentMsg}, nil
	case ConsentBlocked:
		return nil, core.NewError(core.ErrUnauthorized, consentMsg)
	case ConsentContinue:
		// No consent action needed — proceed.
	}

	// Stage 4b: Bot command dispatch.
	if pc.isBotCommand && p.botCmds != nil {
		if result, matched := p.botCmds.TryHandle(ctx, pc.input.Content, pc.session); matched {
			pc.tr.Annotate("bot_command", true)
			p.storeTrace(ctx, pc.tr, pc.session.ID, "", pc.cfg.ChannelLabel)
			return result, nil
		}
	}

	// Short-followup expansion (Rust parity: contextualize_short_followup).
	pc.content = pc.input.Content
	if pc.cfg.ShortFollowupExpansion {
		pc.content, pc.correctionTurn = ContextualizeShortFollowup(pc.session, pc.content)
	}

	return nil, nil
}

// ── Stage 5: User message storage ─────────────────────────────────────

func (p *Pipeline) stageMessageStorage(ctx context.Context, pc *pipelineContext) error {
	pc.tr.BeginSpan("message_storage")
	pc.msgID = db.NewID()
	topicTag := p.deriveTopicTag(pc.session, pc.content)
	_, err := p.store.ExecContext(ctx,
		`INSERT INTO session_messages (id, session_id, role, content, topic_tag)
		 VALUES (?, ?, 'user', ?, ?)`,
		pc.msgID, pc.session.ID, pc.content, topicTag,
	)
	if err != nil {
		pc.tr.EndSpan("error")
		return core.WrapError(core.ErrDatabase, "failed to store user message", err)
	}
	pc.session.AddUserMessage(pc.content)
	pc.tr.Annotate("msg_id", pc.msgID)
	pc.tr.Annotate("topic_tag", topicTag)
	pc.tr.Annotate("turn_count", pc.session.TurnCount())
	pc.tr.EndSpan("ok")
	return nil
}

// ── Stage 6: Turn creation ─────────────────────────────────────────────

func (p *Pipeline) stageTurnCreation(ctx context.Context, pc *pipelineContext) {
	pc.turnID = db.NewID()
	_, turnErr := p.store.ExecContext(ctx,
		`INSERT INTO turns (id, session_id) VALUES (?, ?)`,
		pc.turnID, pc.session.ID,
	)
	if turnErr != nil {
		log.Warn().Err(turnErr).Str("turn", pc.turnID).Msg("turn creation failed, continuing")
	}
}

// ── Stage 7: Decomposition gate + 7.5 task synthesis ──────────────────

func (p *Pipeline) stageDecomposition(_ context.Context, pc *pipelineContext) {
	pc.tr.BeginSpan("decomposition_gate")
	p.tasks.Start(pc.taskID, pc.msgID)
	var verificationSubgoalsHint []string
	if pc.cfg.DecompositionGate {
		d := EvaluateDecomposition(pc.content, len(pc.session.Messages()))
		pc.decomp = &d
		verificationSubgoalsHint = append(verificationSubgoalsHint, d.Subtasks...)
		p.tasks.Classify(pc.taskID, TaskClassification(pc.decomp.Decision))
		pc.tr.Annotate("decision", pc.decomp.Decision.String())
		if pc.decomp.Decision == DecompDelegated && len(pc.decomp.Subtasks) > 0 {
			pc.tr.Annotate("subtask_count", len(pc.decomp.Subtasks))
			p.tasks.Delegate(pc.taskID, pc.input.AgentID, nil)
			log.Debug().
				Str("task", pc.taskID).
				Str("session", pc.session.ID).
				Str("agent", pc.input.AgentID).
				Int("subtasks", len(pc.decomp.Subtasks)).
				Msg("task delegated via decomposition gate")
		}
	} else {
		pc.decomp = &DecompositionResult{Decision: DecompCentralized}
	}
	pc.tr.EndSpan("ok")

	// Stage 7.5: Task state synthesis (Rust: synthesize_task_state + plan).
	if pc.cfg.TaskOperatingState != "" || pc.cfg.DecompositionGate {
		pc.tr.BeginSpan("task_synthesis")
		// Populate agent skills from DB (Rust: list_skills → filter enabled).
		var agentSkills []string
		if p.store != nil {
			skillNames := SkillRegistryNamesFromDB(p.store)
			for name := range skillNames {
				agentSkills = append(agentSkills, name)
			}
		}
		pc.synthesis = SynthesizeTaskState(pc.content, pc.session.TurnCount(), agentSkills)
		AnnotateTaskStateTrace(pc.tr, pc.synthesis)
		pc.tr.EndSpan("ok")

		gateDecision := MapPlannedAction(pc.synthesis, pc.decomp)
		switch gateDecision {
		case ActionGateDelegate:
			if pc.decomp.Decision == DecompCentralized {
				pc.decomp.Decision = DecompDelegated
				log.Debug().Str("session", pc.session.ID).Msg("planner upgraded decision to delegation")
			}
		case ActionGateSpecialistPropose:
			if pc.decomp.Decision == DecompCentralized {
				pc.decomp.Decision = DecompSpecialistProposal
				log.Debug().Str("session", pc.session.ID).Msg("planner upgraded decision to specialist proposal")
			}
		}
	}

	if len(verificationSubgoalsHint) == 0 {
		verificationSubgoalsHint = verificationSubgoals(pc.content)
	}
	if pc.session != nil {
		pc.session.SetTaskVerificationHints(
			pc.synthesis.Intent,
			pc.synthesis.Complexity,
			pc.synthesis.PlannedAction,
			verificationSubgoalsHint,
		)
	}
}

// ── Stage 8: Authority resolution ──────────────────────────────────────

func (p *Pipeline) stageAuthority(_ context.Context, pc *pipelineContext) {
	pc.tr.BeginSpan("authority_resolution")
	pc.secClaim = ResolveSecurityClaim(pc.cfg.AuthorityMode, pc.input.Claim)
	if pc.threatCaution && pc.secClaim.Authority == core.AuthorityCreator {
		pc.secClaim.Authority = core.AuthorityPeer
		pc.secClaim.ThreatDowngraded = true
		log.Warn().Str("session", pc.session.ID).Msg("authority reduced due to injection caution")
	}
	pc.session.Authority = pc.secClaim.Authority
	pc.session.SecurityClaim = &pc.secClaim
	pc.tr.Annotate("authority", pc.secClaim.Authority.String())
	if len(pc.secClaim.Sources) > 0 {
		sourceStrs := make([]string, len(pc.secClaim.Sources))
		for i, s := range pc.secClaim.Sources {
			sourceStrs[i] = s.String()
		}
		pc.tr.Annotate("claim_sources", strings.Join(sourceStrs, ","))
	}
	pc.tr.EndSpan("ok")
}

// ── Stage 8.5: Memory retrieval ────────────────────────────────────────

func (p *Pipeline) stageMemoryRetrieval(ctx context.Context, pc *pipelineContext) {
	retrievalStrat := DecideRetrievalStrategy(pc.synthesis, pc.session.TurnCount(), 2048)
	if p.retriever != nil && retrievalStrat.Strategy != "none" {
		pc.tr.BeginSpan("memory_retrieval")
		pc.memoryBlock = p.retriever.Retrieve(ctx, pc.session.ID, pc.content, retrievalStrat.Budget)
		if pc.memoryBlock != "" {
			pc.session.SetMemoryContext(pc.memoryBlock)
		}
		if p.store != nil {
			index := agenttools.BuildMemoryIndex(ctx, p.store, 20, pc.content)
			if index != "" {
				pc.session.SetMemoryIndex(index)
			} else {
				pc.session.SetMemoryIndex("[Memory Index: No memories stored yet. " +
					"Memories will accumulate as conversations continue. " +
					"When a user asks about a past topic, use search_memories(query) to check, " +
					"or be honest that you don't have stored memories about it yet.]")
			}
		}
		fragmentCount := 0
		if pc.memoryBlock != "" {
			fragmentCount = strings.Count(pc.memoryBlock, "---") + 1
		}

		// Personality reinforcement on early turns (Rust parity).
		if pc.memoryBlock == "" && pc.session.TurnCount() <= 3 {
			personalityBoost := "[Identity Reinforcement] This is an early turn in the conversation. " +
				"Your personality, voice, and behavioral directives from the system prompt are " +
				"your PRIMARY guide for tone, style, and approach. Embody them fully — do not " +
				"fall back to generic AI assistant behavior. Respond as the character defined in " +
				"your system prompt, not as a generic helpful assistant."
			pc.session.AddSystemMessage(personalityBoost)
			pc.tr.Annotate("personality_boost", true)
		}

		AnnotateRetrievalStrategy(pc.tr, retrievalStrat.Strategy, retrievalStrat.Budget, fragmentCount)

		// Enriched memory trace: tier breakdown and budget consumption.
		tiersQueried := []string{retrievalStrat.Strategy}
		hitsPerTier := map[string]int{retrievalStrat.Strategy: fragmentCount}
		budgetConsumed := 0
		if pc.memoryBlock != "" {
			budgetConsumed = llm.EstimateTokens(pc.memoryBlock)
			for _, tier := range []string{"episodic", "semantic", "working", "procedural"} {
				count := strings.Count(pc.memoryBlock, "["+tier+"]")
				if count > 0 {
					if _, exists := hitsPerTier[tier]; !exists {
						tiersQueried = append(tiersQueried, tier)
					}
					hitsPerTier[tier] = count
				}
			}
		}
		AnnotateMemoryTrace(pc.tr, tiersQueried, hitsPerTier, budgetConsumed)
		pc.tr.Annotate(TraceNSRetrieval+".reason", retrievalStrat.Reason)
		pc.tr.EndSpan("ok")
	}
}

// ── Stage 9: Delegated execution ───────────────────────────────────────

func (p *Pipeline) stageDelegation(ctx context.Context, pc *pipelineContext) (*Outcome, error) {
	if pc.cfg.DelegatedExecution && pc.decomp.Decision == DecompDelegated && len(pc.decomp.Subtasks) > 0 {
		pc.tr.BeginSpan("delegated_execution")
		delegOutcome := p.executeDelegation(ctx, pc.session, pc.decomp, pc.turnID)
		if delegOutcome != nil {
			AnnotateDelegationTrace(pc.tr, pc.input.AgentID, len(pc.decomp.Subtasks), "decomposition_gate")
			if delegOutcome.Complete {
				pc.tr.Annotate("delegation_complete", true)
				pc.tr.EndSpan("ok")
				p.storeTrace(ctx, pc.tr, pc.session.ID, pc.msgID, pc.cfg.ChannelLabel)
				p.tasks.Complete(pc.taskID)
				return &Outcome{
					SessionID:  pc.session.ID,
					MessageID:  pc.msgID,
					Content:    delegOutcome.Content,
					ReactTurns: delegOutcome.Turns,
				}, nil
			}
			pc.delegationResult = delegOutcome.Content
			pc.tr.Annotate("delegation_complete", false)
			pc.tr.Annotate("delegation_threaded", true)
			log.Debug().Str("session", pc.session.ID).Int("quality", delegOutcome.Quality.Score).Msg("delegation incomplete, threading to inference")
		}
		pc.tr.EndSpan("fallthrough")
	}
	return nil, nil
}

// ── Stage 10: Skill-first fulfillment ──────────────────────────────────

func (p *Pipeline) stageSkillFirst(ctx context.Context, pc *pipelineContext) (*Outcome, error) {
	pc.tr.BeginSpan("skill_dispatch")
	if skillResult := p.trySkillFirst(ctx, pc.cfg, pc.secClaim.Authority, pc.session, pc.content); skillResult != nil {
		pc.tr.Annotate("matched", true)
		pc.tr.EndSpan("ok")
		p.storeTrace(ctx, pc.tr, pc.session.ID, pc.msgID, pc.cfg.ChannelLabel)
		p.tasks.Complete(pc.taskID)
		return p.guardOutcome(pc.cfg, skillResult), nil
	}
	pc.tr.EndSpan("skipped")
	return nil, nil
}

// ── Stage 11: Shortcut dispatch ────────────────────────────────────────

func (p *Pipeline) stageShortcut(ctx context.Context, pc *pipelineContext) (*Outcome, error) {
	pc.tr.BeginSpan("shortcut_dispatch")
	if pc.cfg.ShortcutsEnabled {
		if result := p.tryShortcut(ctx, pc.session, pc.content, pc.correctionTurn, pc.cfg.ChannelLabel); result != nil {
			pc.tr.Annotate("matched", true)
			pc.tr.EndSpan("ok")
			p.recordShortcutCost(ctx, pc.turnID, pc.session.ID, pc.cfg.ChannelLabel)
			p.storeTrace(ctx, pc.tr, pc.session.ID, pc.msgID, pc.cfg.ChannelLabel)
			p.tasks.Complete(pc.taskID)
			return p.guardOutcome(pc.cfg, result), nil
		}
	}
	pc.tr.EndSpan("skipped")
	return nil, nil
}

// ── Stage 11.5: Cache check ────────────────────────────────────────────

func (p *Pipeline) stageCacheCheck(ctx context.Context, pc *pipelineContext) (*Outcome, error) {
	if pc.cfg.CacheEnabled && !pc.input.NoCache {
		pc.tr.BeginSpan("cache_check")
		if hit := p.CheckCache(ctx, pc.content); hit != nil {
			pc.tr.Annotate("cache_hit", true)
			pc.tr.Annotate("cache_model", hit.Model)
			pc.tr.EndSpan("ok")

			cacheOutcome := &Outcome{
				SessionID: pc.session.ID,
				MessageID: pc.msgID,
				Content:   hit.Content,
				Model:     hit.Model,
				FromCache: true,
				inferenceParams: &InferenceParams{
					FromCache:   true,
					ModelActual: hit.Model,
				},
			}
			if p.guards != nil && pc.cfg.CacheGuardSet != GuardSetNone {
				pc.tr.BeginSpan("cache_guard")
				cacheGuardStart := time.Now()
				cacheGuardResult := p.guards.ApplyFull(cacheOutcome.Content)
				cacheOutcome.Content = cacheGuardResult.Content
				cacheGuardDur := time.Since(cacheGuardStart).Milliseconds()
				cacheGuardEntries := make(map[string]GuardTraceEntry)
				for _, v := range cacheGuardResult.Violations {
					parts := strings.SplitN(v, ":", 2)
					name := strings.TrimSpace(parts[0])
					reason := ""
					if len(parts) > 1 {
						reason = strings.TrimSpace(parts[1])
					}
					cacheGuardEntries[name] = GuardTraceEntry{Outcome: "fail", Reason: reason}
				}
				AnnotateGuardTrace(pc.tr, cacheGuardEntries, "cached", cacheGuardDur)
				pc.tr.EndSpan("ok")
			}

			// Persist cached assistant response to session_messages.
			assistantMsgID := db.NewID()
			topicTag := p.deriveTopicTag(pc.session, cacheOutcome.Content)
			_, cacheStoreErr := p.store.ExecContext(ctx,
				`INSERT INTO session_messages (id, session_id, role, content, topic_tag)
				 VALUES (?, ?, 'assistant', ?, ?)`,
				assistantMsgID, pc.session.ID, cacheOutcome.Content, topicTag,
			)
			if cacheStoreErr != nil {
				log.Error().Err(cacheStoreErr).Str("session", pc.session.ID).Msg("failed to store cached assistant message")
			}
			pc.session.AddAssistantMessage(cacheOutcome.Content, nil)

			p.storeTraceWithArtifacts(ctx, pc.tr, pc.session.ID, pc.msgID, pc.cfg.ChannelLabel, cacheOutcome)
			p.tasks.Complete(pc.taskID)
			return cacheOutcome, nil
		}
		pc.tr.EndSpan("miss")
	}
	return nil, nil
}

// ── Stage 11.75: Prepare for inference ─────────────────────────────────

func (p *Pipeline) stagePrepareInference(ctx context.Context, pc *pipelineContext) {
	pc.tr.BeginSpan("prepare_inference")
	p.PrepareForInference(ctx, pc.session, pc.memoryBlock, pc.cfg.BudgetTier)

	// Annotate context budget allocation so the dashboard shows where tokens go.
	{
		budget := defaultTokenBudget
		switch pc.cfg.BudgetTier {
		case 0:
			budget = 4096
		case 2:
			budget = 16384
		case 3:
			budget = 32768
		}
		var sysToks, memToks, histToks int
		for _, m := range pc.session.Messages() {
			msgTokens := llm.EstimateTokens(m.Content)
			switch m.Role {
			case "system":
				sysToks += msgTokens
			case "user", "assistant":
				histToks += msgTokens
			}
		}
		if pc.memoryBlock != "" {
			memToks = llm.EstimateTokens(pc.memoryBlock)
		}
		AnnotateContextBudgetTrace(pc.tr, budget, sysToks, 0, memToks, histToks)
	}

	// Annotate routing decision: which model was selected and why.
	{
		var candidates []string
		var winner string
		var winnerScore float64
		routingMode := "fallback"

		if pc.cfg.ModelOverride != "" {
			winner = pc.cfg.ModelOverride
			routingMode = "override"
		} else if p.llmSvc != nil && p.llmSvc.Router() != nil {
			router := p.llmSvc.Router()
			for _, t := range router.Targets() {
				candidates = append(candidates, t.Model)
			}
			userContent := ""
			msgs := pc.session.Messages()
			for i := len(msgs) - 1; i >= 0; i-- {
				if msgs[i].Role == "user" {
					userContent = msgs[i].Content
					break
				}
			}
			target := router.Select(&llm.Request{
				Messages: []llm.Message{{Role: "user", Content: userContent}},
			})
			winner = target.Model
			if router.MetascoreSelector != nil {
				routingMode = "metascore"
			} else {
				routingMode = "heuristic"
			}
		}
		AnnotateRoutingTrace(pc.tr, candidates, winner, winnerScore, routingMode)

		if p.llmSvc != nil && p.llmSvc.Router() != nil {
			w := p.llmSvc.Router().GetRoutingWeights()
			AnnotateRoutingWeightsTrace(pc.tr, map[string]float64{
				"efficacy":     w.Efficacy,
				"cost":         w.Cost,
				"availability": w.Availability,
				"locality":     w.Locality,
				"confidence":   w.Confidence,
				"speed":        w.Speed,
			})
		}
	}
	pc.tr.EndSpan("ok")

	// Thread delegation result into inference context (Rust parity H8).
	if pc.delegationResult != "" {
		pc.session.AddSystemMessage(fmt.Sprintf(
			"[Prior delegation result from orchestrate-subagents]\n%s\n"+
				"[Incorporate the above delegation output into your response. "+
				"If it's incomplete, supplement with your own reasoning.]",
			pc.delegationResult,
		))
	}
}

// ── Stage 12: Inference ────────────────────────────────────────────────

func (p *Pipeline) stageInference(ctx context.Context, pc *pipelineContext) (*Outcome, error) {
	pc.tr.BeginSpan("inference")
	p.dashNotify("stream_start", map[string]string{
		"session_id": pc.session.ID, "agent_id": pc.input.AgentID,
	})
	var outcome *Outcome
	var err error
	switch pc.cfg.InferenceMode {
	case InferenceStandard:
		outcome, err = p.runStandardInferenceWithTrace(ctx, pc.cfg, pc.session, pc.msgID, pc.turnID, pc.tr)
	case InferenceStreaming:
		outcome, err = p.prepareStreamInference(ctx, pc.cfg, pc.session, pc.msgID)
	default:
		pc.tr.EndSpan("error")
		return nil, core.NewError(core.ErrConfig, "unknown inference mode")
	}
	if err != nil {
		pc.tr.EndSpan("error")
		p.storeTrace(ctx, pc.tr, pc.session.ID, pc.msgID, pc.cfg.ChannelLabel)
		return nil, err
	}
	pc.tr.EndSpan("ok")
	return outcome, nil
}

// ── Stage 12.5: Post-inference ─────────────────────────────────────────

func (p *Pipeline) stagePostInference(ctx context.Context, pc *pipelineContext, outcome *Outcome) {
	// Cache store (Rust: store_in_cache).
	if pc.cfg.CacheEnabled && !pc.input.NoCache && outcome != nil && !outcome.Stream && outcome.Content != "" {
		p.bgWorker.Submit("storeCache", func(bgCtx context.Context) {
			p.StoreInCache(bgCtx, pc.content, outcome.Content, outcome.Model)
		})
	}

	// Empty response guard.
	if outcome != nil && strings.TrimSpace(outcome.Content) == "" {
		outcome.Content = "I wasn't able to formulate a response right now. Could you try again?"
		log.Warn().Str("session", pc.session.ID).Msg("pipeline produced empty content — injected fallback")
	}

	p.storeTraceWithArtifacts(ctx, pc.tr, pc.session.ID, pc.msgID, pc.cfg.ChannelLabel, outcome)

	// Mark task completed.
	p.tasks.Complete(pc.taskID)
}
