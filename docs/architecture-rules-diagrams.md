# Architecture Rules Diagrams

These diagrams are the visual companion to
[architecture_rules.md](/Users/jmachen/code/roboticus/architecture_rules.md)
and [ARCHITECTURE.md](/Users/jmachen/code/roboticus/ARCHITECTURE.md).

They are intentionally optimized for:

- thin-connector comprehension
- centralized pipeline ownership
- inward dependency direction
- narrow capability seams
- visual legibility over exhaustiveness

The preferred notation in this file is C4. Supporting diagrams are included
only where a dynamic or rule-oriented view is clearer than a structural one.

## C4 Conventions

This file follows the same C4 conventions used elsewhere in the repo:

- one architectural level per diagram
- explicit relationship labels
- transport adapters shown as adapters, not as owners of behavior
- transport payload normalization owned once per transport, not duplicated
  across route and adapter layers
- extension discovery/init owned once at daemon composition, not split between
  admin install UX and route handlers
- pipeline shown as the central factory
- supporting non-C4 diagrams clearly labeled as such

## 1. C4 Level 1: Architecture Context

This diagram explains the architecture in terms of ownership, not deployment.

```mermaid
C4Context
    title Architecture Context — Thin Connectors Around A Unified Factory

    Person(contributor, "Contributor / AI Assistant", "Adds features, fixes bugs, and reviews changes")

    System(connectors, "Connectors", "HTTP routes, streaming handlers, channel adapters, cron connectors, CLI-facing adapters")
    System(factory, "Unified Pipeline Factory", "Single behavioral authority for agent turns")
    System(caps, "Capability Packages", "Agent, LLM, DB, channel, schedule, MCP, plugin, browser, session, core")
    System(guards, "Architecture Rules + Fitness Tests", "Rules and tests that keep boundaries intact")

    Rel(contributor, connectors, "May modify adapter code")
    Rel(contributor, factory, "May modify shared behavior")
    Rel(connectors, factory, "Parse -> call -> format")
    Rel(factory, caps, "Uses narrow capabilities")
    Rel(guards, connectors, "Constrain connector behavior")
    Rel(guards, factory, "Constrain behavioral ownership")
    Rel(contributor, guards, "Must follow and satisfy")
```

## 2. C4 Level 2: Container Diagram

This is the primary architecture diagram for the ruleset. It shows where
behavior lives and where it MUST NOT live.

```mermaid
C4Container
    title Container Diagram — Connector / Factory / Capability Ownership

    Person(user, "User / Operator", "Interacts through channels, API, dashboard, and CLI")

    System_Boundary(runtime, "Goboticus") {
        Container(connectors, "Connector Layer", "Go", "Routes, streaming handlers, cron triggers, channel adapters, CLI adapters. Owns parse/call/format only.")
        Container(pipeline, "Unified Pipeline Factory", "Go", "Single behavioral authority for agent turns. Owns session, consent, task state, delegation, inference preparation, guards, ingest, trace, and parity.")
        Container(agent, "Agent Capabilities", "Go", "Tool execution, policy, orchestration, memory ingestion, runtime execution seams")
        Container(llm, "LLM Capabilities", "Go", "Routing, fallback, cache, breaker, inference execution, streaming")
        Container(state, "State And Runtime Capabilities", "Go + SQLite", "DB, sessions, memory, schedule, channel delivery, MCP, plugin, browser, core")
        Container(guards, "Architecture Enforcement", "Go tests + docs", "Rules, fitness tests, dependency checks, ownership checks")
    }

    Rel(user, connectors, "Uses", "HTTP / SSE / WS / channel protocol / CLI")
    Rel(connectors, pipeline, "Invokes", "pipeline.RunPipeline(...) or subscribes to EventBus")
    Rel(pipeline, agent, "Uses", "Go interfaces")
    Rel(pipeline, llm, "Uses", "Go interfaces")
    Rel(pipeline, state, "Uses", "Go interfaces + SQL")
    Rel(guards, connectors, "Constrains", "Fitness tests + rules")
    Rel(guards, pipeline, "Constrains", "Fitness tests + rules")
    Rel(guards, state, "Constrains", "Dependency DAG checks")
```

## 3. C4 Level 3: Component Diagram — Connector Layer

This is the clearest visual statement of the thin-connector rule.

```mermaid
C4Component
    title Component Diagram — Connector Layer Must Stay Thin

    Container_Boundary(connectors, "Connector Layer") {
        Component(api, "HTTP / REST Routes", "Go", "Parse HTTP input, call unified pipeline, format HTTP output")
        Component(streaming, "Streaming / SSE Routes", "Go", "Parse stream request, call unified pipeline, format chunked output")
        Component(ws, "WebSocket Transport", "Go", "Ticket auth, topic subscription, EventBus broadcast. No business logic.")
        Component(channels, "Channel Adapters", "Go", "Translate Telegram, Discord, Signal, WhatsApp, Email, Voice, A2A to pipeline input. Own canonical transport normalization for those protocols.")
        Component(cron, "Cron Connectors", "Go", "Translate scheduled execution into pipeline input. Manual 'run now' must reuse the durable worker lifecycle, not bypass it.")
        Component(cli, "CLI / Admin Adapters", "Go", "Call API or runtime surfaces without owning shared business rules")
    }

    Container_Ext(pipeline, "Unified Pipeline Factory", "Go", "Single behavioral authority")

    Rel(api, pipeline, "Calls", "RunPipeline")
    Rel(streaming, pipeline, "Calls", "RunPipeline")
    Rel(ws, pipeline, "Subscribes to EventBus", "topic push")
    Rel(channels, pipeline, "Calls", "RunPipeline")
    Rel(cron, pipeline, "Calls", "RunPipeline")
    Rel(cli, pipeline, "Calls shared behavior through canonical surfaces", "Go / HTTP")
```

## 4. C4 Level 3: Component Diagram — Unified Pipeline Factory

This diagram shows what the architecture rules mean by "the pipeline owns
behavior."

```mermaid
C4Component
    title Component Diagram — Unified Pipeline Owns Shared Behavior

    Container_Boundary(pipeline, "Unified Pipeline Factory (v1.0.4: 16 named stages)") {
        Component(defense, "Input Defense", "Go", "stageValidation, stageInjectionDefense, stageDedup — injection defense, normalization, transport-independent safety")
        Component(session, "Session And Consent", "Go", "stageSessionResolution, stageUserMessage, stageTurnCreation — session resolution, continuity, cross-channel consent")
        Component(task, "Task State And Delegation", "Go", "stageDecomposition, stageAuthority, stageDelegation — task synthesis, decomposition, delegation, specialist flow")
        Component(dispatch, "Dispatch And Context", "Go", "stageSkillFirst, stageShortcut, stageCache — skill-first, shortcut dispatch, cache prep")
        Component(infer, "Inference Orchestration", "Go", "stageInference — model selection via SessionEscalationTracker, fallback, standard or streaming")
        Component(post, "Post-Turn Processing", "Go", "stagePostInference — 26-guard chain (incl. FinancialActionTruthGuard), ingest, trace, cost, persistence")
    }

    Container_Ext(connectors, "Connector Layer", "Routes and adapters")
    Container_Ext(agent, "Agent Capabilities", "Tool execution, policy, orchestration")
    Container_Ext(llm, "LLM Capabilities", "Routing, cache, breaker, inference")
    Container_Ext(state, "State And Runtime Capabilities", "DB, memory, sessions, schedule, channel delivery")

    Rel(connectors, defense, "Supplies normalized input")
    Rel(defense, session, "Passes validated input")
    Rel(session, task, "Passes resolved turn context")
    Rel(task, dispatch, "Passes task and delegation state")
    Rel(dispatch, infer, "Passes prepared request")
    Rel(infer, post, "Passes raw result")

    Rel(task, agent, "Uses", "Go interfaces")
    Rel(dispatch, state, "Uses", "Go interfaces + SQL")
    Rel(infer, llm, "Uses", "Go interfaces")
    Rel(post, state, "Uses", "Go interfaces + SQL")
```

## 5. C4 Level 3: Component Diagram — Capability Narrowing

This diagram captures the intended replacement for broad service bags.

```mermaid
C4Component
    title Component Diagram — Narrow Capability Seams

    Container_Boundary(root, "Composition Root") {
        Component(appstate, "AppState", "Go", "Assembly root and adapter source; not a universal stage dependency")
        Component(stageDeps, "Stage Dependency Bundles", "Go", "Narrow capability sets tailored to individual stages")
    }

    Container_Boundary(stages, "Pipeline / Agent Stages") {
        Component(stageA, "Stage A", "Go", "Consumes only the capabilities it uses")
        Component(stageB, "Stage B", "Go", "Consumes only the capabilities it uses")
        Component(stageC, "Stage C", "Go", "Consumes only the capabilities it uses")
    }

    Container_Ext(llm, "LLM Service", "Go")
    Container_Ext(db, "Store / DB", "Go + SQLite")
    Container_Ext(runtime, "Runtime Services", "Go", "Policy, tools, MCP, plugin, browser, scheduler, channels")

    Rel(appstate, stageDeps, "Adapts into")
    Rel(stageDeps, stageA, "Provides narrow deps to")
    Rel(stageDeps, stageB, "Provides narrow deps to")
    Rel(stageDeps, stageC, "Provides narrow deps to")

    Rel(appstate, llm, "Owns")
    Rel(appstate, db, "Owns")
    Rel(appstate, runtime, "Owns")
```

## 6. Supplementary Rule View — Security Claim And Sandbox Ownership

This view captures a runtime seam that was easy to misunderstand during parity
work: claim resolution is pipeline-owned, while sandbox enforcement is shared
across policy evaluation and tool/runtime path resolution. The important rule
is that those seams must agree on the operator-visible contract. Post-inference
guards are not allowed to invent a softer or harsher denial surface than the
actual tool/policy result; they may suppress fabricated capability claims, but
they must preserve real policy/sandbox denials as truth.

```mermaid
flowchart LR
    connector["Connector / Route / Channel"]
    stage8["Stage 8: authority_resolution"]
    session["Session Runtime Context\n(channel, workspace, allowed paths snapshot,\nsecurity claim)"]
    policy["Policy Engine\n(tool allow/deny, path_protection,\nconfig_protection)"]
    toolrt["Tool Runtime Path Resolution\nResolvePath / ValidatePath"]
    guards["Post-Inference Guards"]
    operator["Operator-visible Outcome"]

    connector --> stage8 --> session
    session --> policy
    session --> toolrt
    policy --> operator
    toolrt --> operator
    guards --> operator

    %% Rule note:
    %% guards may remove fabricated "I can't..." language,
    %% but they must not overwrite real policy/sandbox denials or
    %% fabricate canned execution summaries in their place.
```

## 7. Supplementary Rule View — Release Control Plane

This view captures the release-distribution seam that `v1.0.6` exposed. The
operator-facing release is not the git tag by itself; it is the full published
control plane from tag through public site.

Two source-tree artifacts are part of that control plane before publication:

- `docs/releases/vX.Y.Z-release-notes.md`
- `CHANGELOG.md` section `## [X.Y.Z]`

If either is missing for the tagged version, the release is malformed and the
publication path must stop before claiming a live operator-facing release.

```mermaid
flowchart LR
    tag["Git tag"]
    gate["Tag-gated release checks"]
    ghrel["GitHub Release object\nassets + SHA256SUMS.txt"]
    latest["GitHub releases/latest"]
    dispatch["Site sync trigger"]
    sitesync["roboticus.ai release-sync"]
    deploy["Production deploy"]
    installers["/install.sh + /install.ps1"]
    upgrade["roboticus update/upgrade"]
    operator["Operator install / upgrade"]

    tag --> gate --> ghrel --> latest
    ghrel --> dispatch --> sitesync --> deploy
    sitesync --> installers
    latest --> installers
    latest --> upgrade
    installers --> operator
    upgrade --> operator

    %% Rule notes:
    %% - A tag without ghrel is not a release.
    %% - Public installers must be copied from the tagged source repo,
    %%   not maintained as a divergent site-local fork.
    %% - Site sync may not depend on source-tree paths that are not part
    %%   of the tagged release contract.
```

## 8. Supplementary Rule View — Streaming Is Not A Separate Product

This is a supporting diagram rather than a C4 view because it expresses a
behavioral equivalence rule.

```mermaid
flowchart LR
    subgraph Standard["Standard Delivery"]
        s1["Parse"]
        s2["Format"]
    end

    subgraph Streaming["Streaming Delivery"]
        t1["Parse"]
        t2["Chunk Format"]
    end

    shared["Shared Pre-Inference Pipeline Path"]

    s1 --> shared --> s2
    t1 --> shared --> t2
```

## 8. Supplementary Rule View — Channel Ingress Ownership

Webhook-capable channels follow the same thin-connector rule more strictly than
before: the route owns HTTP framing and pipeline dispatch, while the adapter
owns transport verification and payload normalization. Routes must not carry a
second copy of Telegram / WhatsApp webhook JSON parsing once the adapter
defines the canonical ingress contract.

```mermaid
flowchart LR
    http["Webhook Route"]
    verify["Adapter Verification\n(challenge / signature)"]
    normalize["Adapter Normalization\ntransport JSON -> InboundMessage batch"]
    bridge["Route Bridge\nInboundMessage -> pipeline.Input"]
    pipeline["RunPipeline(...)"]

    http --> verify --> normalize --> bridge --> pipeline
```

## 8.5 Supplementary Rule View — Extension Runtime Ownership

Plugin administration and plugin runtime are not the same thing. Install/search
surfaces may write plugin files or inspect catalogs, but the live runtime must
own registry construction, directory discovery, manifest parsing, init, and
install-time hot loading. Routes consume that runtime-owned registry; they do
not create their own view of plugin state. Manifest-backed plugin scripts and
skill scripts also share one core execution contract for containment,
interpreter allowlists, output limits, and sandbox env shaping.

```mermaid
flowchart LR
    install["Install / Catalog UX"]
    fs["Plugin Directory"]
    daemon["Daemon Composition Root"]
    registry["plugin.Registry\n(scan + init + hot load + active statuses)"]
    routes["Plugin Routes / Dashboard"]
    runtime["Runtime Tool Surface"]

    install --> fs
    install --> registry
    daemon --> registry
    fs --> daemon
    registry --> routes
    registry --> runtime
```

## 9. Supplementary Rule View — Request Construction Ownership

This view captures the validated v1.0.6 ownership rule for the inference
artifact. Tool selection, memory preparation, checkpoint restore, and prompt
assembly all converge into one `llm.Request`. The builder may compact or
compress older conversational history, but it must preserve the latest user
message and the higher-value system/memory surfaces.

```mermaid
flowchart LR
    stage8["Stage 8 / 8.5\nAuthority + Memory Preparation"]
    prune["Tool Pruning Stage"]
    session["Session Runtime Artifacts\nSelectedToolDefs\nMemoryContext\nMemoryIndex\nVerificationEvidence"]
    builder["ContextBuilder.BuildRequest"]
    request["Final llm.Request"]
    router["Model Selection / Inference"]

    stage8 --> session
    prune --> session
    session --> builder --> request --> router

    note1["Latest user message survives verbatim"]
    note2["Prompt-layer tool roster matches structured tool defs"]
    note3["Empty compacted history messages are dropped before inference"]
    note4["Prompt compression is disabled for v1.0.6\nafter failed history-bearing soak"]

    builder -.-> note1
    builder -.-> note2
    builder -.-> note3
    builder -.-> note4
```

## 10. Supplementary Rule View — Continuity And Learning Ownership

This view captures the validated v1.0.6 continuity rule. Post-turn artifacts
must be written from turn-owned evidence first, then promoted through explicit
consolidation seams. Reflection is not allowed to invent durable state from
weak proxies when structured turn artifacts already exist.

## 11. Supplementary Rule View — Observability Route Ownership

This view captures the final v1.0.6 route-family contract for trace surfaces.

```mermaid
flowchart LR
    summary["/api/traces\nsummary/search/detail list family"]
    observability["/api/observability/traces\nobservability page / waterfall family"]
    ws["WebSocket topic snapshots"]
    handlers["Canonical HTTP handlers"]
    release["Release notes / architecture docs"]

    summary --> handlers
    observability --> handlers
    ws --> handlers
    release --> handlers
```

```mermaid
flowchart LR
    inference["Inference Turn"]
    traces["Turn Artifacts\ntool_calls\npipeline_traces\nmodel_selection_events"]
    post["Post-Turn Pipeline\nreflection + executive growth + checkpoint policy"]
    episodic["episodic_memory\ncontent + content_json"]
    executive["Executive / Working State"]
    checkpoint["CheckpointRepository\nsave / load / prune"]
    consolidate["Consolidation / Distillation"]
    semantic["semantic_memory"]
    facts["knowledge_facts"]

    inference --> traces --> post
    post --> episodic
    post --> executive
    post --> checkpoint
    episodic --> consolidate
    consolidate --> semantic
    consolidate --> facts

    note["Structured artifacts are authoritative;\ncompact text summaries are for human readability, not downstream reparsing"]
    episodic -.-> note
```

## 11. Supplementary View — WebSocket Topic Subscription (v1.0.3+)

The WebSocket layer is a push-only delivery connector. It does not call
`RunPipeline()` — it subscribes to the EventBus that the pipeline publishes to.

```mermaid
sequenceDiagram
    participant D as Dashboard (Browser)
    participant WS as WS Transport
    participant EB as EventBus
    participant P as Pipeline
    participant DB as SQLite

    D->>WS: Upgrade + ticket
    WS->>WS: Validate ticket (anti-CSRF)
    D->>WS: subscribe(topics=["sessions","traces"])

    Note over P: User message arrives via HTTP/channel
    P->>DB: Persist session, trace, etc.
    P->>EB: Publish(topic="sessions", payload)
    P->>EB: Publish(topic="traces", payload)

    EB->>WS: Deliver "sessions" event
    EB->>WS: Deliver "traces" event
    WS->>D: Push session update
    WS->>D: Push trace update
```

## 12. Supplementary Rule View — No Symptom Fixes

This is a supporting debugging diagram rather than a structural one.

```mermaid
flowchart TD
    bug["Behavior Diverges Across Surfaces"]
    working["Trace Working Path"]
    broken["Trace Broken Path"]
    diff["Identify Shared Divergence"]
    shared["Fix Shared Pipeline / Shared Capability"]
    verify["Verify All Surfaces Inherit The Fix"]

    wrong1["Patch Broken Connector"]
    wrong2["Remove Feature From Working Path"]
    wrong3["Copy Logic Across Connectors"]

    bug --> working
    bug --> broken
    working --> diff
    broken --> diff
    diff --> shared --> verify

    diff -. "MUST NOT" .-> wrong1
    diff -. "MUST NOT" .-> wrong2
    diff -. "MUST NOT" .-> wrong3
```

## 13. Supplementary Rule View — Enforcement Model

This diagram shows how the architecture is kept real.

```mermaid
flowchart LR
    rules["Rules Docs"]
    tests["Fitness + Behavioral Tests"]
    review["Review Checklist"]
    code["Repository Code"]

    rules --> tests
    rules --> review
    tests --> code
    review --> code
```

## 14. Reading Guide

- Use the C4 context and container views to understand architectural ownership.
- Use the connector-layer component diagram when reviewing route, streaming,
  cron, channel, or CLI changes.
- Use the pipeline component diagram when deciding whether behavior belongs in
  the factory.
- Use the capability diagram when evaluating stage dependencies and service-bag
  creep.
- Use the supporting diagrams when validating streaming parity, debugging
  divergence, checking request-artifact ownership, continuity/learning
  ownership, or explaining why a local connector patch is incorrect.

If a proposed code change does not fit cleanly onto these diagrams, the change
SHOULD be treated as architecturally suspect until its ownership becomes clear.

## 14. Memory Retrieval Architecture (v1.0.1+)

Two-stage pattern: direct injection for cheap/session-scoped data, index for
everything else. The model uses tools (`recall_memory`, `search_memories`) to
fetch full content on demand.

```mermaid
sequenceDiagram
    participant U as User Message
    participant P as Pipeline
    participant R as Retriever
    participant DB as SQLite
    participant CB as ContextBuilder
    participant M as Model (LLM)
    participant T as search_memories / recall_memory

    U->>P: "Do you remember palm?"
    P->>R: RetrieveDirectOnly(session, query, budget)
    R->>DB: SELECT from working_memory (session-scoped)
    R->>DB: SELECT from episodic_memory (last 2 hours)
    R-->>CB: [Working Memory] + [Recent Activity]

    P->>DB: BuildMemoryIndex(store, 20, "palm")
    Note over DB: Strategy 1: LIKE on memory_index.summary WHERE '%palm%'
    Note over DB: Strategy 2: FTS5 MATCH on memory_fts JOIN memory_index
    Note over DB: Fill remaining with tier-priority top-N
    DB-->>CB: [Memory Index] with Palm entries in first 1/3

    CB->>M: System prompt + Working + Ambient + Index + History
    M->>T: search_memories(query="palm")
    T->>DB: FTS5 MATCH + LIKE fallback (all tiers)
    DB-->>T: 21 results
    T-->>M: Matching memories with source IDs
    M->>T: recall_memory(id="idx-obsidian-Projects/Pal")
    T->>DB: SELECT full content from source tier
    DB-->>T: Full Palm USD project details
    T-->>M: Complete memory content
    M-->>U: Response with real Palm memories
```

### What Gets Injected vs. What Requires Tool Calls

| Layer | Injection | Source |
|-------|-----------|--------|
| Working Memory | **Direct** (always) | `working_memory` table, session-scoped |
| Recent Activity | **Direct** (always) | `episodic_memory` last 2 hours |
| Memory Index | **Direct** (query-aware) | `memory_index` top-20 + FTS matches |
| Episodic details | **Tool** (`recall_memory`) | `episodic_memory` by ID |
| Semantic facts | **Tool** (`recall_memory`) | `semantic_memory` by ID |
| Procedural stats | **Tool** (`recall_memory`) | `procedural_memory` by ID |
| Relationship data | **Tool** (`recall_memory`) | `relationship_memory` by ID |
| Topic search | **Tool** (`search_memories`) | FTS5 + LIKE across all tiers |

---

## 15. Agentic Retrieval Architecture (v1.0.5)

```
User Query
    │
    ▼
┌────────────────────┐
│ Intent Classifier   │ ← 9 categories (centroid-based)
└────────┬───────────┘
         │
         ▼
┌────────────────────┐
│ Query Decomposer   │ ← splits compound queries into subgoals
└────────┬───────────┘
         │
         ▼
┌────────────────────┐
│ Retrieval Router   │ ← selects tiers + modes per subgoal
│ (11 routing plans) │
└────────┬───────────┘
         │
    ┌────┴────────────────┐
    │  Per-Tier Retrieval  │
    │ ┌─────┐ ┌─────┐     │
    │ │Epis.│ │Sem. │ ... │ ← BM25 + vector hybrid per tier
    │ └──┬──┘ └──┬──┘     │
    └────┼───────┼────────┘
         │       │
         ▼       ▼
┌────────────────────┐
│ Reranker / Filter  │ ← discard weak, boost authority, detect collapse
└────────┬───────────┘
         │
         ▼
┌────────────────────────────────────────┐
│ Context Assembly                       │
│ [Working State] ← direct injection     │
│ [Evidence]      ← ranked with scores   │
│ [Gaps]          ← missing tiers        │
│ [Contradictions]← conflicting entries  │
└────────┬───────────────────────────────┘
         │
         ▼
    LLM Reasoning Engine
         │
         ▼
    Post-Turn:
    ├── Reflection (episode summary → episodic_memory)
    ├── Procedure Detection (tool sequences → learned_skills)
    └── Consolidation (dreaming: promote, decay, prune)
```

### Memory Type Roles

| Memory | Question Answered | Retrieval Method | Searched? |
|--------|-------------------|-----------------|-----------|
| Semantic | "What is true?" | BM25 + vector hybrid | Yes (via router) |
| Episodic | "What happened before?" | FTS + recency union | Yes (via router) |
| Procedural | "How do I do this?" | Keyword + learned skills | Yes (via router) |
| Relationship | "Who is involved?" | Keyword lookup | Yes (via router) |
| Working | "What am I doing now?" | N/A — direct injection | **No** — active state |
