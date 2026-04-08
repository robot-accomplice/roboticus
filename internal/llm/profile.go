package llm

// ModelProfile aggregates runtime signals about a model's fitness for routing.
// All 6 dimensions correspond to the spider graph axes in the dashboard.
type ModelProfile struct {
	Model        string  `json:"model"`
	Provider     string  `json:"provider"`
	Quality      float64 `json:"quality"`      // 0-1, from QualityTracker (= Efficacy axis)
	Availability float64 `json:"availability"` // 0-1, from circuit breaker state
	Cost         float64 `json:"cost"`         // 0-1 normalized cost (lower = cheaper)
	Locality     float64 `json:"locality"`     // 1.0 = local, 0.0 = cloud
	Confidence   float64 `json:"confidence"`   // observation-count penalty
	Speed        float64 `json:"speed"`        // 0-1, from LatencyTracker
}

// RoutingWeights holds the 6 user-configurable axis weights for metascore.
// These correspond to the routingProfile persisted in runtime_settings
// and the spider graph in the dashboard.
type RoutingWeights struct {
	Efficacy     float64
	Cost         float64
	Availability float64
	Locality     float64
	Confidence   float64
	Speed        float64
}

// DefaultRoutingWeights returns the default weight distribution.
func DefaultRoutingWeights() RoutingWeights {
	return RoutingWeights{
		Efficacy: 0.35, Cost: 0.20, Availability: 0.25,
		Locality: 0.10, Confidence: 0.10, Speed: 0.10,
	}
}

// Metascore computes a weighted composite score using default weights.
// Higher is better.
func (p ModelProfile) Metascore() float64 {
	return p.MetascoreWith(DefaultRoutingWeights())
}

// MetascoreWith computes a weighted composite score using custom weights.
// Cost is inverted: lower cost = higher score. All other axes: higher = better.
func (p ModelProfile) MetascoreWith(w RoutingWeights) float64 {
	return w.Efficacy*p.Quality +
		w.Availability*p.Availability +
		w.Cost*(1.0-p.Cost) +
		w.Locality*p.Locality +
		w.Confidence*p.Confidence +
		w.Speed*p.Speed
}

const confidenceThreshold = 10 // minimum observations before full confidence

// BuildModelProfiles constructs profiles from runtime state.
func BuildModelProfiles(
	targets []RouteTarget,
	quality *QualityTracker,
	latency *LatencyTracker,
	capacity *CapacityTracker,
	breakers *BreakerRegistry,
) []ModelProfile {
	profiles := make([]ModelProfile, 0, len(targets))
	for _, t := range targets {
		p := ModelProfile{
			Model:    t.Model,
			Provider: t.Provider,
		}

		// Quality from tracker.
		if quality != nil {
			p.Quality = quality.EstimatedQuality(t.Model)
		} else {
			p.Quality = 0.5
		}

		// Availability from circuit breaker.
		if breakers != nil {
			cb := breakers.Get(t.Provider)
			switch cb.State() {
			case CircuitClosed:
				p.Availability = 1.0
			case CircuitHalfOpen:
				p.Availability = 0.5
			default:
				p.Availability = 0.0
			}
		} else {
			p.Availability = 1.0
		}

		// Cost: normalize to 0-1 range (cap at $0.10/1K tokens).
		p.Cost = t.Cost / 0.0001
		if p.Cost > 1.0 {
			p.Cost = 1.0
		}
		if p.Cost < 0 {
			p.Cost = 0
		}

		// Locality.
		if t.IsLocal {
			p.Locality = 1.0
		}

		// Confidence: penalize cold-start models with few observations.
		if quality != nil {
			obs := quality.ObservationCount(t.Model)
			if obs >= confidenceThreshold {
				p.Confidence = 1.0
			} else {
				p.Confidence = float64(obs) / float64(confidenceThreshold)
			}
		} else {
			p.Confidence = 0.5
		}

		// Speed: from latency tracker (6th spider graph axis).
		if latency != nil {
			p.Speed = latency.SpeedScore(t.Model, 500, 5000)
		} else {
			p.Speed = 0.5
		}

		profiles = append(profiles, p)
	}
	return profiles
}

// MetascoreBreakdown holds the RAW per-axis dimension scores [0, 1] for a model,
// plus the weighted final score. Each dimension field is the raw score for that
// axis — NOT the weighted contribution. This matches the Rust reference format
// and enables per-model, per-dimension relative scoring for dashboard display.
type MetascoreBreakdown struct {
	// Efficacy is the raw quality/accuracy score [0, 1]. From QualityTracker.
	Efficacy float64 `json:"efficacy"`
	// Cost is normalized inverse cost [0, 1]. 1.0 = free, 0.0 = most expensive.
	Cost float64 `json:"cost"`
	// Availability is breaker health * capacity headroom [0, 1].
	Availability float64 `json:"availability"`
	// Locality is task-complexity-adjusted local preference [0, 1].
	Locality float64 `json:"locality"`
	// Confidence is cold-start penalty [0.6, 1.0]. 1.0 = sufficient observations.
	Confidence float64 `json:"confidence"`
	// Speed is measured latency score [0, 1]. 1.0 = fastest.
	Speed float64 `json:"speed"`
	// SessionPenalty is the in-session performance penalty [0, 0.5].
	// Applied as: final_score *= (1.0 - session_penalty).
	SessionPenalty float64 `json:"session_penalty"`
	// FinalScore is the weighted combination of all dimensions.
	FinalScore float64 `json:"final_score"`
}

// Total returns the final weighted score (alias for FinalScore).
func (b MetascoreBreakdown) Total() float64 {
	return b.FinalScore
}

// Breakdown computes the full metascore breakdown with raw dimension scores
// and a weighted final score. This matches the Rust reference's
// ModelProfile::metascore() method.
//
// The complexity parameter [0, 1] affects the locality dimension:
// simple tasks favor local models, complex tasks favor cloud.
// If costAware is true, cost weight increases at the expense of efficacy.
func (p ModelProfile) Breakdown(w RoutingWeights) MetascoreBreakdown {
	return p.BreakdownWithComplexity(0.5, false, w)
}

// BreakdownWithComplexity computes the full metascore breakdown with
// task-complexity-aware locality scoring and optional cost awareness.
func (p ModelProfile) BreakdownWithComplexity(complexity float64, costAware bool, w RoutingWeights) MetascoreBreakdown {
	// Efficacy: raw quality score [0, 1].
	efficacy := p.Quality
	if efficacy < 0 {
		efficacy = 0
	}
	if efficacy > 1 {
		efficacy = 1
	}

	// Cost: sigmoid-normalized inverse. Free → 1.0, expensive → ~0.0.
	// Combined per-1k cost = Cost field * 0.1 (denormalize from profile).
	cost := 1.0 - p.Cost // p.Cost is already 0-1 where 0=free, 1=expensive
	if cost < 0 {
		cost = 0
	}

	// Availability: product of breaker health and capacity headroom.
	availability := p.Availability

	// Locality: local models get a bonus for simple tasks, cloud for complex.
	var locality float64
	if p.Locality == 1.0 {
		// Local model: advantage fades with complexity
		locality = max64(0, 1.0-complexity*0.4)
	} else {
		// Cloud model: advantage grows with complexity
		locality = min64(1.0, complexity*0.4)
	}

	// Speed: from measured latency observations [0, 1].
	speed := p.Speed

	// Confidence: from observation count.
	confidence := p.Confidence

	// Weight selection (matches Rust reference).
	wEff, wCost, wSpeed, wAvail, wLocal := w.Efficacy, w.Cost, w.Speed, w.Availability, w.Locality
	if costAware {
		wEff = 0.25
		wCost = 0.25
		wSpeed = 0.10
		wAvail = 0.20
		wLocal = 0.10
	}

	rawScore := wEff*efficacy +
		wCost*cost +
		wSpeed*speed +
		wAvail*availability +
		wLocal*locality

	finalScore := rawScore * confidence

	return MetascoreBreakdown{
		Efficacy:     efficacy,
		Cost:         cost,
		Availability: availability,
		Locality:     locality,
		Confidence:   confidence,
		Speed:        speed,
		FinalScore:   finalScore,
	}
}

func max64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// SelectByMetascore picks the highest-metascore model, skipping blocked ones.
// Models with Availability <= 0 are always excluded (matching Rust's filter).
func SelectByMetascore(profiles []ModelProfile, breakers *BreakerRegistry) *ModelProfile {
	var best *ModelProfile
	var bestScore float64

	for i := range profiles {
		p := &profiles[i]

		// Skip unavailable models (Rust: .filter(|p| p.availability > 0.0)).
		if p.Availability <= 0 {
			continue
		}

		// Skip blocked providers via circuit breaker.
		if breakers != nil {
			cb := breakers.Get(p.Provider)
			if !cb.Allow() {
				continue
			}
		}

		score := p.Metascore()
		if best == nil || score > bestScore {
			best = p
			bestScore = score
		}
	}
	return best
}

// SelectByMetascoreWeighted picks the highest-scoring model using custom weights.
// Unlike SelectByMetascore, this variant does not consult circuit breakers — the
// caller is responsible for filtering unavailable providers beforehand.
func SelectByMetascoreWeighted(profiles []ModelProfile, weights RoutingWeights) *ModelProfile {
	var best *ModelProfile
	var bestScore float64

	for i := range profiles {
		p := &profiles[i]
		score := p.MetascoreWith(weights)
		if best == nil || score > bestScore {
			best = p
			bestScore = score
		}
	}
	return best
}
