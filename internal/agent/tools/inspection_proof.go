package tools

import "encoding/json"

// InspectionProof is the typed proof emitted by successful read-only
// inspection tools so the loop and RCA can distinguish useful narrowing
// evidence from empty misses without re-parsing tool-specific text output.
type InspectionProof struct {
	ProofType      string `json:"proof_type"`
	InspectionKind string `json:"inspection_kind"`
	ToolName       string `json:"tool_name"`
	Path           string `json:"path,omitempty"`
	Pattern        string `json:"pattern,omitempty"`
	Query          string `json:"query,omitempty"`
	Count          int    `json:"count"`
	Empty          bool   `json:"empty"`
}

func NewInspectionProof(kind, toolName, path string, count int) InspectionProof {
	if count < 0 {
		count = 0
	}
	return InspectionProof{
		ProofType:      "inspection_result",
		InspectionKind: kind,
		ToolName:       toolName,
		Path:           path,
		Count:          count,
		Empty:          count == 0,
	}
}

func (p InspectionProof) WithPattern(pattern string) InspectionProof {
	p.Pattern = pattern
	return p
}

func (p InspectionProof) WithQuery(query string) InspectionProof {
	p.Query = query
	return p
}

func (p InspectionProof) Metadata() json.RawMessage {
	raw, _ := json.Marshal(p)
	return raw
}

func ParseInspectionProof(raw json.RawMessage) (*InspectionProof, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var proof InspectionProof
	if err := json.Unmarshal(raw, &proof); err != nil {
		return nil, false
	}
	if proof.ProofType != "inspection_result" {
		return nil, false
	}
	return &proof, true
}
