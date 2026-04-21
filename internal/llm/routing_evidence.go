package llm

import "strings"

type RoutingScoreRow struct {
	State                  string             `json:"state,omitempty"`
	PrimaryReasonCode      string             `json:"primary_reason_code,omitempty"`
	ReasonCodes            []string           `json:"reason_codes,omitempty"`
	HumanReason            string             `json:"human_reason,omitempty"`
	EvidenceRefs           []string           `json:"evidence_refs,omitempty"`
	PolicySource           string             `json:"policy_source,omitempty"`
	Model                  string             `json:"model"`
	Provider               string             `json:"provider"`
	Tier                   ModelTier          `json:"tier"`
	OrchestratorEligible   bool               `json:"orchestrator_eligible"`
	SubagentEligible       bool               `json:"subagent_eligible"`
	EligibilityReason      string             `json:"eligibility_reason,omitempty"`
	RoleEligible           bool               `json:"role_eligible"`
	RequestEligible        bool               `json:"request_eligible"`
	ToolCapable            bool               `json:"tool_capable"`
	ExclusionReasons       []string           `json:"exclusion_reasons,omitempty"`
	GlobalObservations     int                `json:"global_observations"`
	IntentObservations     int                `json:"intent_observations"`
	CapabilityEvidence     string             `json:"capability_evidence,omitempty"`
	EvidenceRecommendation string             `json:"evidence_recommendation,omitempty"`
	Selected               bool               `json:"selected"`
	Breakdown              MetascoreBreakdown `json:"breakdown"`
}

type RoutingFeatureSet struct {
	AgentRole                    string           `json:"agent_role,omitempty"`
	TurnWeight                   string           `json:"turn_weight,omitempty"`
	TaskIntent                   string           `json:"task_intent,omitempty"`
	TaskComplexity               string           `json:"task_complexity,omitempty"`
	IntentClass                  string           `json:"intent_class,omitempty"`
	MessageCount                 int              `json:"message_count"`
	ToolCount                    int              `json:"tool_count"`
	Complexity                   float64          `json:"complexity"`
	Weights                      RoutingWeights   `json:"weights"`
	RequestEligibleCandidates    []string         `json:"request_eligible_candidates,omitempty"`
	ExcludedCandidates           []map[string]any `json:"excluded_candidates,omitempty"`
	IgnoredForEvidenceModels     []string         `json:"ignored_for_evidence_models,omitempty"`
	IgnoredForEvidenceSuggestion string           `json:"ignored_for_evidence_suggestion,omitempty"`
}

func (s *Service) routingAssessment(req *Request, selectedModel string) ([]RoutingScoreRow, RoutingFeatureSet) {
	if s == nil || s.router == nil || req == nil {
		return nil, RoutingFeatureSet{}
	}
	profiles := BuildModelProfiles(s.router.Targets(), s.quality, s.latency, nil, s.breakers)
	weights := s.router.GetRoutingWeights()
	assessments := assessTargetsForRequest(s.router.Targets(), req, s.toolAllowlist, s.toolBlocklist)
	assessmentByModel := make(map[string]RouteTargetAssessment, len(assessments))
	requestEligibleCandidates := make([]string, 0, len(assessments))
	excludedCandidates := make([]map[string]any, 0)
	for _, assessment := range assessments {
		assessmentByModel[canonicalModelKey(assessment.Model)] = assessment
		assessmentByModel[canonicalModelKey(assessment.Target.Model)] = assessment
		if spec := ModelSpecForTarget(assessment.Target); spec != "" {
			assessmentByModel[canonicalModelKey(spec)] = assessment
		}
		if assessment.RequestEligible {
			requestEligibleCandidates = append(requestEligibleCandidates, assessment.Model)
			continue
		}
		if len(assessment.ExclusionReasons) > 0 {
			excludedCandidates = append(excludedCandidates, map[string]any{
				"model":   assessment.Model,
				"reasons": append([]string(nil), assessment.ExclusionReasons...),
			})
		}
	}

	intentClass := requestIntentClass(req)
	type seenSet map[string]struct{}
	ignoredForEvidence := make([]string, 0)
	unexercisedForEvidence := make([]string, 0)
	unprovenForIntent := make([]string, 0)
	seenIgnored := make(seenSet)
	rows := make([]RoutingScoreRow, 0, len(profiles))
	complexity := requestRoutingComplexity(req)
	requestWeights := weights
	if req.TurnWeight != "" {
		requestWeights = routingWeightsForRequest(weights, req.TurnWeight, req.TaskIntent, req.TaskComplexity)
	}

	for _, p := range profiles {
		if intentClass != "" && s.intentQuality != nil {
			p.Quality = s.intentQuality.EstimatedQualityForIntentTarget(p.Provider, p.Model, intentClass)
		}
		applyIntentEvidence(&p, intentClass, s.intentQuality)
		target := routeTargetForProfile(s.router.Targets(), p)
		assessment := routeTargetAssessmentForProfile(assessmentByModel, target, p)
		recommendation := capabilityEvidenceRecommendation(p, intentClass)
		selected := canonicalModelKey(ModelSpecForTarget(target)) == canonicalModelKey(selectedModel) || canonicalModelKey(p.Model) == canonicalModelKey(selectedModel)
		if !selected && assessment.RequestEligible && (p.CapabilityEvidence == "unexercised" || p.CapabilityEvidence == "unproven_for_intent") {
			key := canonicalModelKey(p.Model)
			if _, exists := seenIgnored[key]; !exists {
				seenIgnored[key] = struct{}{}
				ignoredForEvidence = append(ignoredForEvidence, p.Model)
				switch p.CapabilityEvidence {
				case "unexercised":
					unexercisedForEvidence = append(unexercisedForEvidence, p.Model)
				case "unproven_for_intent":
					unprovenForIntent = append(unprovenForIntent, p.Model)
				}
			}
		}
		rows = append(rows, RoutingScoreRow{
			State:                  target.State,
			PrimaryReasonCode:      target.PrimaryReasonCode,
			ReasonCodes:            append([]string(nil), target.ReasonCodes...),
			HumanReason:            target.HumanReason,
			EvidenceRefs:           append([]string(nil), target.EvidenceRefs...),
			PolicySource:           target.PolicySource,
			Model:                  p.Model,
			Provider:               p.Provider,
			Tier:                   p.Tier,
			OrchestratorEligible:   target.OrchestratorEligible,
			SubagentEligible:       target.SubagentEligible,
			EligibilityReason:      target.EligibilityReason,
			RoleEligible:           routeTargetEligibleForRole(target, req.AgentRole),
			RequestEligible:        assessment.RequestEligible,
			ToolCapable:            assessment.ToolCapable,
			ExclusionReasons:       append([]string(nil), assessment.ExclusionReasons...),
			GlobalObservations:     p.GlobalObservationCount,
			IntentObservations:     p.IntentObservationCount,
			CapabilityEvidence:     p.CapabilityEvidence,
			EvidenceRecommendation: recommendation,
			Selected:               selected,
			Breakdown:              p.BreakdownWithComplexity(complexity, false, requestWeights),
		})
	}

	ignoredSuggestion := ""
	switch {
	case len(unexercisedForEvidence) > 0 && len(unprovenForIntent) > 0 && intentClass != "":
		ignoredSuggestion = "The following models are currently being ignored because they have no runtime evidence yet: " +
			strings.Join(unexercisedForEvidence, ", ") +
			". The following models have runtime history but have not yet been exercised for " + intentClass + ": " +
			strings.Join(unprovenForIntent, ", ")
	case len(unexercisedForEvidence) > 0:
		if intentClass != "" {
			ignoredSuggestion = "The following models are currently being ignored because they have no runtime evidence yet for " + intentClass + ": " + strings.Join(unexercisedForEvidence, ", ")
		} else {
			ignoredSuggestion = "The following models are currently being ignored because they have no runtime evidence yet: " + strings.Join(unexercisedForEvidence, ", ")
		}
	case len(unprovenForIntent) > 0 && intentClass != "":
		ignoredSuggestion = "The following models are currently being ignored because they have runtime history but have not yet been exercised for " + intentClass + ": " + strings.Join(unprovenForIntent, ", ")
	case len(unprovenForIntent) > 0:
		ignoredSuggestion = "The following models are currently being ignored because they do not yet have intent-specific capability evidence: " + strings.Join(unprovenForIntent, ", ")
	}

	return rows, RoutingFeatureSet{
		AgentRole:                    normalizeAgentRole(req.AgentRole),
		TurnWeight:                   req.TurnWeight,
		TaskIntent:                   req.TaskIntent,
		TaskComplexity:               req.TaskComplexity,
		IntentClass:                  intentClass,
		MessageCount:                 len(req.Messages),
		ToolCount:                    len(req.Tools),
		Complexity:                   complexity,
		Weights:                      requestWeights,
		RequestEligibleCandidates:    requestEligibleCandidates,
		ExcludedCandidates:           excludedCandidates,
		IgnoredForEvidenceModels:     ignoredForEvidence,
		IgnoredForEvidenceSuggestion: ignoredSuggestion,
	}
}

func (s *Service) routingChainEventDetails(req *Request, candidates []string) map[string]any {
	details := map[string]any{
		"requested_model":    req.Model,
		"tool_count":         len(req.Tools),
		"chain_len":          len(candidates),
		"agent_role":         normalizeAgentRole(req.AgentRole),
		"turn_weight":        req.TurnWeight,
		"task_intent":        req.TaskIntent,
		"task_complexity":    req.TaskComplexity,
		"intent_class":       requestIntentClass(req),
		"routing_complexity": requestRoutingComplexity(req),
		"complexity_source":  requestRoutingComplexitySource(req),
	}
	details["candidates"] = candidates
	_, features := s.routingAssessment(req, req.Model)
	if len(features.RequestEligibleCandidates) > 0 {
		details["request_eligible_candidates"] = append([]string(nil), features.RequestEligibleCandidates...)
	}
	if len(features.ExcludedCandidates) > 0 {
		details["excluded_candidates"] = append([]map[string]any(nil), features.ExcludedCandidates...)
	}
	if len(features.IgnoredForEvidenceModels) > 0 {
		details["ignored_for_missing_capability_evidence"] = append([]string(nil), features.IgnoredForEvidenceModels...)
	}
	if suggestion := strings.TrimSpace(features.IgnoredForEvidenceSuggestion); suggestion != "" {
		details["capability_evidence_recommendation"] = suggestion
	}
	return details
}
