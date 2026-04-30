package agent

import (
	"roboticus/internal/llm"
)

// Completer wraps an LLM service for the ReAct loop. It is re-exported
// from internal/llm so callers in this package can refer to a single
// `agent.Completer` symbol without importing both packages.
//
// HISTORICAL NOTE: this file used to define an `ExecutionRegistry`
// composition seam (Tools/Policy/Approvals/Plugins/MCP/Browser) plus a
// parallel `agent.ApprovalManager` and a `BrowserExecutor` interface. The
// v1.0.8 tool-execution audit confirmed they were never instantiated and
// never wired into the agent loop — the loop dispatches directly through
// `internal/agent/tools.Registry` and consults `internal/agent/policy`
// for approval classification. The dead surfaces were removed so the
// architectural rules diagrams (`6.6 Supplementary Rule View — Tool
// Execution Admission Ownership`) can describe a single, real admission
// path instead of two parallel candidates.
type Completer = llm.Completer
