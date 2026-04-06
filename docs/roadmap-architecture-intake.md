# Roboticus Roadmap Intake For Roboticus Architecture

This document is not a parity checklist.

Its purpose is to pull the still-relevant Roboticus roadmap into Roboticus so
we can make near-term architectural decisions without painting ourselves into a
corner.

## Sources

- `/Users/jmachen/code/roboticus/docs/ROADMAP.md`
- `/Users/jmachen/code/roboticus/docs/roadmaps/capabilities-roadmap-2026.md`
- `/Users/jmachen/code/roboticus/docs/releases/v0.11.4-spec.md`
- `/Users/jmachen/code/roboticus/ARCHITECTURE_RULES.md`

## How To Read This

- **Baseline contract** lives in `docs/feature-complete-checklist.md`
- **Forward architectural pressure** lives here

An item belongs in this document when:

1. it is not yet fully implemented in Roboticus, and
2. it could change current architecture decisions if ignored

## Immediate Architecture Drivers

These are the roadmap items most likely to influence Roboticus architecture in
the next wave of work.

### 1. Integrations Management (`1.21`)

Roboticus source:
- `ROADMAP.md` item `1.21`

Why it matters:
- pushes us toward a real integration-management domain instead of route-local
  config editing
- requires clean separation between config persistence, secret storage,
  health/testing, and UI/CLI surfaces

Architecture implication:
- Roboticus should not keep growing ad hoc integration routes
- we need a dedicated integration service boundary with:
  - provider/channel metadata
  - connection test API
  - keystore-backed secret writes
  - health/status snapshots

Must preserve:
- route handlers stay thin
- secret writes go through keystore/service boundary
- dashboard and CLI read the same source of truth

### 2. External Browser Runtime Support (`1.19`)

Roboticus source:
- `ROADMAP.md` item `1.19`

Why it matters:
- forces a browser backend abstraction now, before browser logic hardens around
  one runtime

Architecture implication:
- Roboticus browser support should be built behind a backend interface:
  - native browser backend
  - external `agent-browser`-compatible backend
- policy, provenance, and tool surfaces must be backend-agnostic

Must preserve:
- browser actions remain tool/policy governed
- backend selection is explicit and observable
- no connector owns browser business policy

### 3. Built-In CLI Agent Skills (`1.24`)

Roboticus source:
- `ROADMAP.md` item `1.24`

Why it matters:
- introduces typed wrappers around external AI CLIs
- pressures the skill/plugin boundary, sandboxing model, and approval policy

Architecture implication:
- CLI-agent integrations should not be implemented as generic shell escape
- each external CLI should be a typed skill/plugin adapter with:
  - explicit parameters
  - timeout/output limits
  - policy classification
  - structured result parsing

Must preserve:
- no route-level direct shell orchestration
- skill execution remains governed by shared policy
- sandbox and provenance metadata remain first-class

### 4. OpenAPI + Swagger Surface (`1.25`)

Roboticus source:
- `ROADMAP.md` item `1.25`

Why it matters:
- pushes API contracts toward schema-first behavior
- makes route drift and placeholder semantics much more expensive

Architecture implication:
- Roboticus should treat request/response shapes as stable contracts
- doc generation and route tests should converge on a single source of truth

Must preserve:
- no undocumented route-only behavior
- no fake-success endpoints
- auth and operator surfaces remain machine-describable

### 5. Routing Profiles + Spider Graph (`1.26`, `2.19`)

Roboticus source:
- `ROADMAP.md` items `1.26`, `2.19`
- `v0.11.4-spec.md`

Why it matters:
- user weighting, session-aware routing, contextual metascores, and explainable
  routing are all part of the product direction

Architecture implication:
- routing must remain a separable subsystem with:
  - pluggable scoring backends
  - user/profile weighting inputs
  - deterministic audit trail
  - service-path fidelity from selection to execution

Must preserve:
- router output must fully determine execution target
- profile weights must be explicit inputs, not dashboard-only decoration
- efficacy must be testable against baseline routing

### 6. Voice / Multimodal Channels (`1.6`, `2.21`)

Roboticus source:
- `ROADMAP.md` items `1.6`, `2.21`

Why it matters:
- forces a shared speech/media abstraction
- impacts channel adapter contracts, storage, session history, and dashboard/TUI
  observability

Architecture implication:
- channel adapters must not encode bespoke STT/TTS logic
- Roboticus needs shared provider abstractions for:
  - speech-to-text
  - text-to-speech
  - multimodal message parts
  - media storage/reference handling

Must preserve:
- feature parity across channels where claimed
- multimodal content remains part of the shared message model
- observability includes provider, latency, cost, and fallback

### 7. TUI Parity Contract

Roboticus source:
- `ROADMAP.md`, Subroutine P — TUI

Why it matters:
- if Roboticus adopts TUI parity as a product promise, web-only ad hoc APIs
  become architectural debt immediately

Architecture implication:
- operator surfaces should be capability-oriented, not dashboard-specific
- UI-specific composition belongs above a shared operator API surface

Must preserve:
- no dashboard-only hidden state
- all operator-critical actions are exposed through shared APIs/services
- new dashboard features are mapped deliberately to TUI or explicitly excluded

## Secondary Architecture Drivers

These are slightly farther out but still important enough to influence current
design choices.

### 8. Skills Catalog / Registry / Forge (`2.14`, `2.22`, `P.10`)

Why it matters:
- pushes us toward signed manifests, dependency metadata, trust scoring, and
  install/update lifecycle management

Implication:
- skills need stable metadata schemas and registry-aware lifecycle boundaries

### 9. Agent Delegation / Workflow Graphs (`P.6`, `P.12`, `3.3`)

Why it matters:
- moves orchestration from linear delegation toward graph/state workflows

Implication:
- delegation telemetry and subagent contracts should remain structured and
  graph-friendly
- pipeline traces should preserve enough provenance to support future workflow
  visualization

### 10. Service Revenue / Treasury Intelligence (`2.5`, `2.18`, `2.20`)

Why it matters:
- financial features become platform features, not isolated wallet helpers

Implication:
- service catalog, payments, settlement, treasury state, and risk analytics
  should be distinct domains with explicit boundaries

### 11. Dashboard Modularization / Theme Marketplace (`2.27`, `2.28`, `2.29`)

Why it matters:
- current monolithic dashboard patterns become a drag on every operator feature

Implication:
- new UI features should avoid deepening single-file coupling
- theme/profile systems need stable manifest APIs rather than hard-coded UI
  assumptions

## Current Recommendations For Roboticus

These architectural rules should guide work starting now:

1. Treat integrations, routing, browser runtime, and operator UX as shared
   subsystems, not route collections.
2. Keep business logic in pipeline/core packages; never solve upcoming roadmap
   pressure by enriching connectors.
3. Preserve backend abstractions for browser, speech, routing, and external
   tool execution.
4. Prefer typed manifests and typed capability metadata over ad hoc JSON blobs
   whenever a feature is likely to become marketplace/registry driven.
5. Design operator surfaces so dashboard, CLI, and future TUI can share them.
6. Do not let current parity work hard-code assumptions that block:
   - user-weighted routing profiles
   - external browser backend support
   - typed external CLI skills
   - multimodal/speech provider routing
   - modular operator UX

## Concrete Near-Term Planning Set

If we had to choose the smallest roadmap slice that should shape Roboticus
architecture immediately, it would be:

1. `1.21` Integrations management
2. `1.19` External browser runtime support
3. `1.24` Built-in CLI agent skills
4. `1.25` OpenAPI + Swagger contract
5. `1.26` + `2.19` routing profiles / spider graph / metascore explainability
6. `1.6` + `2.21` multimodal + voice abstractions
7. TUI parity contract if Roboticus plans to claim it

These items should be treated as **architecture-shaping**, even if we do not
implement them in the next sprint.
