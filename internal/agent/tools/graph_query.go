// graph_query.go exposes the persisted KnowledgeGraph (knowledge_facts) as an
// agent tool so the model can ask multi-hop structural questions directly —
// "who depends on X?", "what's the path from A to B?", "what transitive
// dependencies does C have?" — without waiting for the retrieval tier to
// surface graph evidence as a side effect of semantic search.
//
// Three operations are supported:
//   - path(from, to, max_depth) — shortest undirected edge chain up to
//     max_depth hops, or "no path" when none exists within the bound.
//   - impact(seed, max_depth) — reverse traversal returning every node that
//     transitively depends on the seed, grouped by distance.
//   - dependencies(seed, max_depth) — forward traversal returning every
//     node the seed transitively depends on.
//
// Tool output is JSON so the model can act on it directly, but also carries
// a human-readable summary line for trace and audit logs.

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	agentmemory "roboticus/internal/agent/memory"
	"roboticus/internal/db"
)

// GraphQueryTool wraps the persisted KnowledgeGraph API in an agent tool.
type GraphQueryTool struct {
	store *db.Store
	// maxFacts caps the number of knowledge_facts rows loaded into memory.
	// Zero means "all facts". Defaults to 500 in NewGraphQueryTool so very
	// large graphs stay responsive.
	maxFacts int
	// maxDepthAllowed hard-caps user-supplied max_depth values so a bad tool
	// call cannot trigger a full graph walk. Defaults to 8 hops.
	maxDepthAllowed int
}

// NewGraphQueryTool constructs the tool with sensible defaults.
func NewGraphQueryTool(store *db.Store) *GraphQueryTool {
	return &GraphQueryTool{
		store:           store,
		maxFacts:        500,
		maxDepthAllowed: 8,
	}
}

// Name satisfies Tool.
func (t *GraphQueryTool) Name() string { return "query_knowledge_graph" }

// Description satisfies Tool.
func (t *GraphQueryTool) Description() string {
	return "Query the persisted knowledge graph over knowledge_facts. Supports three operations: " +
		"\"path\" (shortest edge chain between two nodes), \"impact\" (who transitively depends on a seed), " +
		"and \"dependencies\" (what a seed transitively depends on). Use this for multi-hop structural " +
		"questions instead of semantic search when the agent needs dependency / causality / impact analysis."
}

// Risk satisfies Tool.
func (t *GraphQueryTool) Risk() RiskLevel { return RiskSafe }

// ParameterSchema satisfies Tool.
func (t *GraphQueryTool) ParameterSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"operation": {
				"type": "string",
				"enum": ["path", "impact", "dependencies"],
				"description": "The graph query to run."
			},
			"from": {
				"type": "string",
				"description": "Starting node for path queries (required when operation=path)."
			},
			"to": {
				"type": "string",
				"description": "Destination node for path queries (required when operation=path)."
			},
			"seed": {
				"type": "string",
				"description": "Seed node for impact / dependencies queries."
			},
			"max_depth": {
				"type": "integer",
				"description": "Max hops to traverse. Defaults: 6 for path, 3 for impact/dependencies. Hard-capped at 8.",
				"minimum": 1
			}
		},
		"required": ["operation"]
	}`)
}

// graphQueryArgs is the parsed parameter bundle. Only fields relevant to the
// chosen operation are required.
type graphQueryArgs struct {
	Operation string `json:"operation"`
	From      string `json:"from"`
	To        string `json:"to"`
	Seed      string `json:"seed"`
	MaxDepth  int    `json:"max_depth"`
}

// graphPathResult is the JSON payload returned for a path query.
type graphPathResult struct {
	Summary string                 `json:"summary"`
	From    string                 `json:"from"`
	To      string                 `json:"to"`
	Depth   int                    `json:"depth"`
	Hops    []graphPathHopJSON     `json:"hops"`
	Found   bool                   `json:"found"`
	Graph   graphStatsJSON         `json:"graph"`
	Extra   map[string]interface{} `json:"extra,omitempty"`
}

type graphPathHopJSON struct {
	From     string  `json:"from"`
	To       string  `json:"to"`
	Relation string  `json:"relation"`
	FactID   string  `json:"fact_id"`
	Score    float64 `json:"confidence"`
}

// graphTraversalResult is the JSON payload returned for impact/dependencies.
type graphTraversalResult struct {
	Summary string               `json:"summary"`
	Seed    string               `json:"seed"`
	Mode    string               `json:"mode"`
	Nodes   []graphNodeJSON      `json:"nodes"`
	Paths   []graphTraversalJSON `json:"paths"`
	Graph   graphStatsJSON       `json:"graph"`
}

type graphNodeJSON struct {
	Name     string `json:"name"`
	MinDepth int    `json:"min_depth"`
}

type graphTraversalJSON struct {
	Depth int                `json:"depth"`
	End   string             `json:"end"`
	Edges []graphPathHopJSON `json:"edges"`
}

type graphStatsJSON struct {
	EdgesLoaded int `json:"edges_loaded"`
	NodesLoaded int `json:"nodes_loaded"`
}

// Execute satisfies Tool. All error paths return a Result so the agent sees
// a useful message instead of a bare Go error — the tool harness prefers
// "soft" errors for user-facing tool calls.
func (t *GraphQueryTool) Execute(ctx context.Context, params string, _ *Context) (*Result, error) {
	if t.store == nil {
		return &Result{Output: "knowledge graph store is not available"}, nil
	}

	var args graphQueryArgs
	if err := json.Unmarshal([]byte(params), &args); err != nil {
		return &Result{Output: "invalid arguments: " + err.Error()}, nil
	}
	args.Operation = strings.ToLower(strings.TrimSpace(args.Operation))

	graph, err := agentmemory.LoadKnowledgeGraphWithLimit(ctx, t.store, t.maxFacts)
	if err != nil {
		return &Result{Output: "failed to load knowledge graph: " + err.Error()}, nil
	}
	stats := graphStatsJSON{
		EdgesLoaded: graph.EdgeCount(),
		NodesLoaded: graph.NodeCount(),
	}

	switch args.Operation {
	case "path":
		return t.runPath(graph, args, stats)
	case "impact":
		return t.runTraversal(graph, args, stats, true)
	case "dependencies":
		return t.runTraversal(graph, args, stats, false)
	case "":
		return &Result{Output: "operation is required: one of path, impact, dependencies"}, nil
	default:
		return &Result{Output: fmt.Sprintf("unknown operation %q; use path, impact, or dependencies", args.Operation)}, nil
	}
}

func (t *GraphQueryTool) runPath(graph *agentmemory.KnowledgeGraph, args graphQueryArgs, stats graphStatsJSON) (*Result, error) {
	from := strings.TrimSpace(args.From)
	to := strings.TrimSpace(args.To)
	if from == "" || to == "" {
		return &Result{Output: "path query requires both from and to"}, nil
	}
	depth := args.MaxDepth
	if depth <= 0 {
		depth = 6
	}
	if depth > t.maxDepthAllowed {
		depth = t.maxDepthAllowed
	}

	edges := graph.ShortestPath(from, to, depth)
	result := graphPathResult{
		From:  from,
		To:    to,
		Depth: len(edges),
		Found: len(edges) > 0,
		Graph: stats,
	}
	for _, edge := range edges {
		result.Hops = append(result.Hops, graphPathHopJSON{
			From:     edge.From,
			To:       edge.To,
			Relation: edge.Fact.Relation,
			FactID:   edge.Fact.ID,
			Score:    edge.Fact.Confidence,
		})
	}
	if result.Found {
		result.Summary = fmt.Sprintf("%s -> %s in %d hop(s)", from, to, len(edges))
	} else {
		result.Summary = fmt.Sprintf("no path from %s to %s within %d hop(s)", from, to, depth)
	}
	return marshalGraphResult(result)
}

func (t *GraphQueryTool) runTraversal(graph *agentmemory.KnowledgeGraph, args graphQueryArgs, stats graphStatsJSON, reverse bool) (*Result, error) {
	seed := strings.TrimSpace(args.Seed)
	if seed == "" {
		return &Result{Output: "impact/dependencies queries require seed"}, nil
	}
	depth := args.MaxDepth
	if depth <= 0 {
		depth = 3
	}
	if depth > t.maxDepthAllowed {
		depth = t.maxDepthAllowed
	}

	var paths []agentmemory.GraphPath
	mode := "dependencies"
	if reverse {
		paths = graph.Impact(seed, depth)
		mode = "impact"
	} else {
		paths = graph.Dependencies(seed, depth)
	}

	result := graphTraversalResult{
		Seed:  seed,
		Mode:  mode,
		Graph: stats,
	}
	seen := make(map[string]int)
	for _, path := range paths {
		edges := make([]graphPathHopJSON, 0, len(path.Edges))
		for _, edge := range path.Edges {
			edges = append(edges, graphPathHopJSON{
				From:     edge.From,
				To:       edge.To,
				Relation: edge.Fact.Relation,
				FactID:   edge.Fact.ID,
				Score:    edge.Fact.Confidence,
			})
		}
		end := path.End()
		result.Paths = append(result.Paths, graphTraversalJSON{
			Depth: path.Depth(),
			End:   end,
			Edges: edges,
		})
		lowered := strings.ToLower(end)
		if prior, ok := seen[lowered]; !ok || path.Depth() < prior {
			seen[lowered] = path.Depth()
		}
	}

	for name, depth := range seen {
		result.Nodes = append(result.Nodes, graphNodeJSON{Name: name, MinDepth: depth})
	}
	if len(result.Nodes) == 0 {
		result.Summary = fmt.Sprintf("no %s found for %s within %d hop(s)", mode, seed, depth)
	} else {
		result.Summary = fmt.Sprintf("found %d %s node(s) within %d hop(s) of %s", len(result.Nodes), mode, depth, seed)
	}
	return marshalGraphResult(result)
}

// marshalGraphResult serialises a graph result struct to a Result payload,
// keeping the Output as compact JSON the agent can parse verbatim.
func marshalGraphResult(result any) (*Result, error) {
	buf, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return &Result{Output: "failed to marshal graph query result: " + err.Error()}, nil
	}
	return &Result{Output: string(buf), Source: "builtin"}, nil
}
