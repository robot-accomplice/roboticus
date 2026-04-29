# Architecture Rules for Contributors and AI Assistants

This document codifies the development requirements for clean code and clean
architecture in Goboticus. It is both a normative rulebook and an
architectural explanation for human contributors and AI coding assistants
working in this repository.

This document supplements, and does not replace, [ARCHITECTURE.md](ARCHITECTURE.md).
If these documents appear to conflict, contributors MUST treat
`ARCHITECTURE.md` as the source of truth for architectural intent and MUST
update both documents to remove the ambiguity before merging.

The visual companion to this document is
[docs/architecture-rules-diagrams.md](docs/architecture-rules-diagrams.md).

Goboticus is built around a simple architectural model:

- connectors are transport adapters
- the pipeline is the single behavioral authority
- packages communicate through narrow interfaces and data contracts
- shared behavior is centralized instead of copied
- business rules are expressed once and inherited everywhere

This document uses RFC 2119 language only for normative requirements.
Rationale and examples are informative unless they explicitly use RFC 2119
keywords.

## 1. RFC 2119 Interpretation

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in RFC 2119.

## 2. Audience and Scope

### Normative Requirements

These rules apply to:

- Human contributors making code, test, or documentation changes
- AI coding assistants proposing or applying changes
- Reviewers evaluating whether a change is architecturally acceptable

These rules apply most strongly to:

- `internal/api/routes/**`
- `internal/pipeline/**`
- connector code in API handlers, streaming handlers, cron connectors, and
  channel adapters
- shared runtime surfaces such as `AppState`, pipeline stage deps, guard
  chains, routing, delegation, and session continuity

### Rationale

These are the areas where Goboticus is most vulnerable to architectural drift.
The common failure mode is not missing code. It is code being added at the
wrong seam.

## 3. Mandatory Pre-Change Workflow

### Normative Requirements

Before changing pipeline or connector code, contributors and AI assistants:

- MUST read [ARCHITECTURE.md](ARCHITECTURE.md)
- MUST verify whether the change belongs in a connector or in the factory
- MUST trace the working path and the broken path before changing behavior
- MUST prefer fixing divergence in the shared pipeline over patching one entry
  point
- MUST consult `git log`, `git show`, or equivalent history when relying on a
  prior behavior

Contributors MUST NOT treat missing context as permission to improvise
architecture.

### Rationale

Goboticus has multiple entry points into the same behavioral core. A bug in
one path is often a divergence from the shared path, not a justification for a
local patch. The correct first question is not "how do I fix this route?" but
"where does this behavior actually belong?"

## 4. Primary Architectural Requirements

### 4.0 No canned user- or operator-facing prose

#### Normative Requirements

Implementations MUST NOT append, substitute, or inject fixed template prose into
agent responses, guard-chain output, tool-result facades shown to users, or
HTTP/API response bodies unless there is no alternative (for example, a body
required verbatim by an external protocol).

Implementations MUST prefer explanations grounded in model-generated text, tool
results, structured errors, logs, or telemetry.

#### Rationale

Canned blocks override the model, hide the real denial or error, and train
operators to distrust output. Policy and diagnostics already belong in tool
results, structured events, and logs; user-visible text should trace to those
sources.

### 4.1 Connector-Factory Boundary

#### Normative Requirements

Connectors MUST do exactly three things:

1. Parse request input into pipeline-friendly structures
2. Call the unified pipeline with a `PipelineConfig` preset
3. Format the resulting `PipelineOutcome`

Connectors MUST NOT:

- resolve sessions
- classify authority
- evaluate decomposition or delegation
- perform skill-first routing
- apply guards
- select models
- execute tools
- perform cache decisions
- contain duplicated business rules from another connector

If a connector contains control flow that is not strictly about parsing,
transport-specific validation, or response formatting, that logic MUST be
presumed to belong in the pipeline until proven otherwise.

#### Rationale

Goboticus uses a factory-oriented architecture. The unified pipeline is the
factory. It takes normalized input, runs the behavioral stages of an agent
turn, and produces an outcome. Connectors are adapters around that factory.
They translate transport shapes into pipeline-friendly input and translate
pipeline outcomes back into transport-specific output.

This boundary is the core clean-architecture seam in the system:

- connectors own protocol shape
- the pipeline owns behavior
- lower-level packages own reusable capabilities

If this boundary is preserved, new channels inherit behavior automatically.
If this boundary is violated, parity drift becomes inevitable.

#### Examples

Acceptable connector logic includes:

- decoding JSON, query params, or channel payloads
- validating transport-specific framing requirements
- mapping HTTP status codes or SSE events
- formatting a response for a specific channel

Unacceptable connector logic includes:

- deciding whether to decompose a task
- choosing a model
- deciding whether memory should be consulted
- applying a guard
- deciding whether delegation should happen
- reproducing the behavior of another connector inline

### 4.2 The Pipeline Is the Factory

#### Normative Requirements

Business logic for agent turns MUST live in the unified pipeline and its
pipeline-owned modules.

For this project, "business logic" includes, at minimum:

- injection defense
- session resolution
- consent handling for cross-channel continuity
- task-state synthesis
- decomposition and delegation
- specialist creation workflows
- skill-first dispatch
- shortcut dispatch
- inference preparation
- inference execution selection
- guard chain application
- post-turn ingest
- nickname refinement
- cost and trace recording

No feature that materially affects agent behavior MAY be implemented in only
one connector unless that exception is explicitly documented in
`ARCHITECTURE.md` and enforced by tests.

#### Rationale

The pipeline is the single behavioral authority for agent turns. It is where
Goboticus expresses:

- what an agent turn is
- how turns are persisted
- how memory, routing, guards, tools, and delegation are invoked
- which stages run for which entry points
- how parity across surfaces is preserved through config rather than
  duplication

The most important architectural question in the repository is:
"Should this behavior be inherited by all entry points?" If the answer is yes,
it belongs in the pipeline.

### 4.3 Feature Parity

#### Normative Requirements

If a capability exists for one agent-turn entry point, contributors SHOULD
assume it is expected to exist for all entry points unless a documented preset
rationale says otherwise.

Differences between API, streaming, channel, and cron behavior MUST be
expressed through `PipelineConfig` or an equivalent pipeline-owned policy
surface. They MUST NOT be encoded by duplicating or omitting business logic in
connectors.

API and dashboard webchannels are distinct connector classes, not interchangeable
paths. `/api/**` routes MUST be treated as externally addressable,
independently permission-controlled control/data surfaces for remote management,
automation, debugging, bootstrap, and explicit API-client actions. Dashboard
webchannels MUST be treated as dashboard-private delivery channels for state
snapshots and UI event streams.

The dashboard MUST NOT use direct API polling as its normal interface control
path. When the dashboard and an API route expose the same domain truth, both
MUST consume the same shared producer/composition seam. A webchannel MUST NOT
reimplement business logic, and an API route MUST NOT become the dashboard's
implicit state bus.

#### Rationale

Feature parity is an architectural property, not just a release goal. When one
entry point gets a capability and another does not, the default assumption
SHOULD be that the architecture has been violated.

### 4.4 Cross-Channel Session Continuity

#### Normative Requirements

Cross-channel session continuity is a security control.

Implementations MUST:

- scope sessions to their originating channel context
- require explicit user consent before another channel accesses that session
- bind consent to the correct requesting principal, not merely the broad
  channel class
- preserve the originating session as the authority source for consent

Implementations MUST NOT:

- grant implicit cross-channel access
- persist session-wide blanket consent when the intent is narrower
- downgrade or bypass consent checks for convenience or UX reasons

#### Rationale

Cross-channel continuity is not a presentation feature. It is a pipeline-owned
security rule because it affects session identity, trust, and continuity across
connectors.

## 5. Package and Dependency Boundaries

### 5.1 Dependency Direction

#### Normative Requirements

Packages MUST communicate through interfaces, data types, and well-defined
module interfaces rather than back-references to higher-level implementation
details.

`internal/api` SHOULD act as an adapter layer. It SHOULD own HTTP, WebSocket,
route parsing, auth extraction, and response formatting. It SHOULD NOT become
the long-term home of pipeline business logic.

`internal/pipeline` SHOULD own the unified pipeline and pipeline-specific
shared logic. Pipeline-owned modules MUST NOT depend back on `internal/api`.

#### Rationale

Goboticus follows clean architecture by keeping dependencies pointed inward
toward reusable behavior and away from transport-specific details.

In practical terms:

- `internal/api` is an adapter layer
- `internal/pipeline` is the behavioral core
- `internal/agent`, `internal/llm`, `internal/channel`, `internal/mcp`,
  `internal/plugin`, `internal/schedule`, `internal/db`, and `internal/core`
  provide capabilities at progressively lower levels

The key test for package direction is replacement:

- can the API be replaced without changing pipeline behavior?
- can a lower-level capability be mocked or swapped without rewriting the
  higher-level owner?
- can a stage be tested with only the deps it uses?

If not, coupling has become too broad.

### 5.2 Dependency Injection and Stage Boundaries

#### Normative Requirements

Stage-specific dependency structs and capability interfaces are the preferred
boundary mechanism for the pipeline.

For live pipeline code:

- each stage SHOULD receive only the capabilities it actually uses
- `AppState` MAY implement capability interfaces as an adapter
- pipeline stages SHOULD NOT accept `AppState` directly once a narrower dep
  struct exists for that stage
- new stage dependency bundles MUST be justified by stage ownership, not
  convenience

Contributors MUST NOT introduce a renamed service bag that recreates `AppState`
under a different type name.

#### Rationale

Narrow dependency bundles prevent the pipeline from turning into a service bag.
Renaming a broad dependency without narrowing it is not architecture; it is
camouflage.

## 6. Clean Code Requirements

### 6.1 Single Responsibility and Cohesion

#### Normative Requirements

Files, modules, and functions SHOULD have one dominant responsibility.

Contributors MUST split code when:

- one file becomes a grab bag for unrelated concerns
- orchestration, persistence, formatting, and policy are mixed together
- a helper exists only to let one large function keep growing

Refactors SHOULD split by concept or stage boundary, not by arbitrary helper
grouping.

#### Rationale

The goal is obvious ownership, not small files for their own sake. A file
SHOULD feel like it has one reason to exist.

### 6.2 Naming and Intent

#### Normative Requirements

Names MUST describe the domain meaning of the code, not just its mechanism.

Contributors SHOULD prefer names that answer:

- what stage this belongs to
- what capability it requires
- what outcome it produces

Names such as `helpers`, `utils`, `misc`, and `manager` SHOULD be avoided
unless the abstraction is genuinely cohesive.

#### Rationale

Good names make architectural seams visible. A contributor SHOULD be able to
look at a file or type name and infer whether it belongs to:

- a connector
- a pipeline stage
- a lower-level capability
- a transport adapter
- a test-only shim

### 6.3 Duplication

#### Normative Requirements

Copying business logic across connectors, stages, or packages is an
architectural defect in this project.

Contributors MUST:

- remove duplicated connector behavior by moving it into pipeline-owned code
- centralize shared policy behind pipeline config or shared stage logic
- avoid parallel implementations of the same rule in multiple layers

Contributors MUST NOT preserve duplication merely because deleting it would
require a broader refactor.

#### Rationale

In Goboticus, duplication across connectors is especially dangerous because it
creates false parity. Two entry points may appear equivalent while quietly
drifting apart over time.

### 6.4 Transitional Code

#### Normative Requirements

Temporary shims, re-exports, compatibility helpers, and migration layers MAY be
introduced during refactors, but they:

- MUST have a clear target state
- SHOULD be removed once canonical ownership is established
- MUST NOT become a permanent excuse for ambiguous ownership

#### Rationale

Compatibility layers are acceptable during migration only if they point toward
a single canonical owner. Transitional code that leaves ownership ambiguous is
architectural debt, not progress.

### 6.5 Comments and Documentation

#### Normative Requirements

Comments SHOULD explain why the code exists, why a boundary matters, or what
invariant must hold.

Comments MUST NOT restate obvious syntax or narrate trivial operations.

When a contributor introduces a non-obvious architectural exception, they MUST
document it in the relevant module and, when applicable, in `ARCHITECTURE.md`.

#### Rationale

Good documentation in this repository explains boundaries and invariants, not
the syntax of the current line.

## 7. Change Design Rules

### 7.1 No Symptom Fixes

#### Normative Requirements

When behavior diverges across channels or paths, contributors MUST:

1. trace the working path
2. trace the broken path
3. identify the divergence
4. fix the divergence at the shared boundary

Contributors MUST NOT:

- remove a feature from the working path to match the broken path
- duplicate the working logic into the broken connector
- paper over a shared bug with a connector-specific special case

#### Rationale

Goboticus has many surfaces over the same engine. Without discipline, bug fixes
naturally drift toward connector-local patches. That is how parity rot happens.

### 7.2 Streaming Is Not a Separate Product

#### Normative Requirements

Streaming is a delivery mode, not a separate architecture.

Pre-inference behavior for streaming and non-streaming paths MUST remain
pipeline-equivalent unless an explicit, documented exception exists.

#### Rationale

Streaming and non-streaming entry points are allowed to differ in delivery, not
in behavioral ownership. If pre-inference logic diverges between them, that
divergence MUST be owned by the pipeline and documented as a preset-level
difference.

### 7.3 Exceptions Must Be Explicit

#### Normative Requirements

Any off-pipeline inference surface, route exemption, or intentional parity
break MUST:

- be documented in `ARCHITECTURE.md`
- have a rationale
- define what pipeline-aligned policy it still honors
- be covered by tests that prevent silent drift

The approved off-pipeline API flows are:

**Configuration interview** — `/api/interview`:

- `/api/interview/start`
- `/api/interview/turn`
- `/api/interview/finish`

**Diagnostic analysis** — post-hoc observability endpoints that call
`llm.Service.Complete()` directly for remediation suggestions on historical
data. These are NOT agent-turn behavior. They honor pipeline-aligned heuristics
via `pipeline.ContextAnalyzer` and MUST NOT evolve into agent-turn paths:

- `AnalyzeSession` (`routes/sessions.go`)
- `AnalyzeTurn` (`routes/turn_detail.go`)

If any additional route, connector, or workflow bypasses `pipeline.RunPipeline()`
for agent-turn behavior, that exemption MUST be added to both
`ARCHITECTURE.md` and this document before merge.

#### Rationale

Undocumented exceptions are one of the fastest ways to erode architectural
trust. The repository SHOULD make unusual seams obvious.

## 8. Testing and Enforcement Requirements

### Normative Requirements

Before merge, changes affecting architecture or shared behavior:

- MUST pass `go test ./...`
- MUST pass `go vet ./...`
- MUST pass the architecture fitness suite

Architecture-sensitive changes SHOULD add or update tests that verify:

- connectors still invoke the unified pipeline
- documented exemptions remain documented
- shared behavior remains parity-consistent across entry points
- capability boundaries do not regress into broad state access

Tests SHOULD assert behavior and ownership, not just text shape. Structural
tests MAY exist, but they SHOULD complement behavioral tests rather than
replace them.

### Rationale

Architecture is not real unless the repository can defend it.

Goboticus uses both structural tests and behavioral tests:

- structural tests protect boundaries and dependency direction
- behavioral tests verify that the intended owner still owns the behavior

Neither is enough alone. Structural tests without behavioral tests can protect
the wrong abstraction. Behavioral tests without structural tests can allow
silent boundary decay.

## 9. Requirements for AI Coding Assistants

### Normative Requirements

AI coding assistants working in this repository:

- MUST NOT add canned template prose to agent-visible surfaces (guards, stitched
  responses, tool facades, APIs) except when unavoidable per an external protocol
- MUST read `ARCHITECTURE.md` before changing pipeline or connector code
- MUST treat connector logic growth as a likely architectural bug
- MUST prefer shared-pipeline fixes over local handler patches
- MUST NOT invent historical behavior when git history can be checked
- MUST surface architectural uncertainty rather than silently guessing
- MUST preserve user-made changes and MUST NOT revert unrelated work

When proposing a change, an AI assistant SHOULD explain:

- why the code belongs in its chosen layer
- how the change preserves connector-factory compliance
- what tests or verifications demonstrate compliance

### Rationale

AI assistants amplify both good and bad architecture quickly. The desired AI
behavior is conservative at the boundary and decisive inside the correct owner.

## 10. Review Checklist

### Normative Requirements

Reviewers, contributors, and AI assistants SHOULD ask all of the following:

- Does this change inject canned user- or operator-facing prose where model or
  tool-grounded output is expected?
- Does this change add business logic to a connector?
- Does this change duplicate a rule that already exists elsewhere?
- If another channel were added tomorrow, would it inherit this behavior
  automatically?
- Does any stage receive broader dependencies than it actually uses?
- Does this refactor reduce coupling, or merely move code around?
- Does the change preserve explicit consent for cross-channel continuity?
- Is there any new undocumented exemption from the pipeline?

If the answer to any of these questions is unfavorable, the change SHOULD be
considered incomplete until corrected or explicitly documented.

### Rationale

This checklist exists so reviews can ask architectural questions directly
instead of relying on taste or intuition.

## 11. Target State

### Normative Requirements

Contributors and AI assistants MUST optimize for the following target state,
even when making small local fixes:

- connectors that are boring and thin
- a pipeline that is the single behavioral authority for agent turns
- package boundaries enforced by interfaces and stage-specific dependencies
- no session-sharing convenience that weakens explicit consent
- code that is easy to move, test, and reason about because ownership is
  obvious

### Rationale

The system SHOULD be considered healthy when:

- connectors are boring
- the pipeline is trusted
- capability seams are narrow
- parity is inherited instead of hand-maintained
- contributors can tell where code belongs without debate
