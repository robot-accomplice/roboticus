package llm

import "math"

// QueryFeatures describes a query for model routing.
type QueryFeatures struct {
	CharCount    int
	MessageCount int
	ToolCount    int
	HasCode      bool
	HasMath      bool
}

// featureVector converts QueryFeatures to a float slice for the logistic model.
func (qf QueryFeatures) featureVector() []float64 {
	code := 0.0
	if qf.HasCode {
		code = 1.0
	}
	mathVal := 0.0
	if qf.HasMath {
		mathVal = 1.0
	}
	return []float64{
		float64(qf.CharCount) / 10000.0, // normalized
		float64(qf.MessageCount) / 50.0,
		float64(qf.ToolCount) / 20.0,
		code,
		mathVal,
	}
}

// LogisticRouter routes queries to model tiers using logistic regression.
type LogisticRouter struct {
	weights []float64
	bias    float64
}

// NewLogisticRouter creates a router with the given learned parameters.
func NewLogisticRouter(weights []float64, bias float64) *LogisticRouter {
	return &LogisticRouter{weights: weights, bias: bias}
}

// DefaultLogisticRouter returns a router with hand-tuned defaults.
func DefaultLogisticRouter() *LogisticRouter {
	return &LogisticRouter{
		weights: []float64{0.3, 0.4, 0.2, 0.5, 0.3}, // charCount, msgCount, toolCount, code, math
		bias:    -0.5,
	}
}

// Route returns the recommended model tier (0.0-1.0 complexity score).
func (lr *LogisticRouter) Route(features QueryFeatures) float64 {
	fv := features.featureVector()
	if len(fv) != len(lr.weights) {
		return 0.5 // default mid-tier
	}
	z := lr.bias
	for i := range lr.weights {
		z += lr.weights[i] * fv[i]
	}
	return sigmoid(z)
}

// RoutingExample is a training sample for the router.
type RoutingExample struct {
	Features QueryFeatures
	Outcome  float64 // 0=simple, 1=complex
}

// Train updates weights using simple gradient descent.
func (lr *LogisticRouter) Train(dataset []RoutingExample, epochs int, learningRate float64) {
	if len(dataset) == 0 {
		return
	}
	for e := 0; e < epochs; e++ {
		for _, ex := range dataset {
			fv := ex.Features.featureVector()
			pred := lr.Route(ex.Features)
			err := ex.Outcome - pred
			for i := range lr.weights {
				lr.weights[i] += learningRate * err * fv[i]
			}
			lr.bias += learningRate * err
		}
	}
}

func sigmoid(x float64) float64 {
	return 1.0 / (1.0 + math.Exp(-x))
}
