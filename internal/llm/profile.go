package llm

// ModelProfile aggregates runtime signals about a model's fitness for routing.
type ModelProfile struct {
	Model        string  `json:"model"`
	Provider     string  `json:"provider"`
	Quality      float64 `json:"quality"`      // 0-1, from QualityTracker
	Availability float64 `json:"availability"` // 0-1, from circuit breaker state
	Cost         float64 `json:"cost"`         // per-output-token cost
	Locality     float64 `json:"locality"`     // 1.0 = local, 0.0 = cloud
	Confidence   float64 `json:"confidence"`   // observation-count penalty
}

// Metascore computes a weighted composite score for routing decisions.
// Higher is better.
func (p ModelProfile) Metascore() float64 {
	return 0.35*p.Quality +
		0.25*p.Availability +
		0.20*(1.0-p.Cost) + // invert: lower cost = higher score
		0.10*p.Locality +
		0.10*p.Confidence
}

const confidenceThreshold = 10 // minimum observations before full confidence

// BuildModelProfiles constructs profiles from runtime state.
func BuildModelProfiles(
	targets []RouteTarget,
	quality *QualityTracker,
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

		profiles = append(profiles, p)
	}
	return profiles
}

// SelectByMetascore picks the highest-metascore model, skipping blocked ones.
func SelectByMetascore(profiles []ModelProfile, breakers *BreakerRegistry) *ModelProfile {
	var best *ModelProfile
	var bestScore float64

	for i := range profiles {
		p := &profiles[i]

		// Skip blocked providers.
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
