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

    Rel(user, connectors, "Uses", "HTTP / SSE / channel protocol / CLI")
    Rel(connectors, pipeline, "Invokes", "pipeline.RunPipeline(...)")
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
        Component(channels, "Channel Adapters", "Go", "Translate Telegram, Discord, Signal, WhatsApp, Email, Voice, A2A to pipeline input")
        Component(cron, "Cron Connectors", "Go", "Translate scheduled execution into pipeline input")
        Component(cli, "CLI / Admin Adapters", "Go", "Call API or runtime surfaces without owning shared business rules")
    }

    Container_Ext(pipeline, "Unified Pipeline Factory", "Go", "Single behavioral authority")

    Rel(api, pipeline, "Calls", "RunPipeline")
    Rel(streaming, pipeline, "Calls", "RunPipeline")
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

    Container_Boundary(pipeline, "Unified Pipeline Factory") {
        Component(defense, "Input Defense", "Go", "Injection defense, normalization, transport-independent safety prechecks")
        Component(session, "Session And Consent", "Go", "Session resolution, continuity, cross-channel consent, turn creation")
        Component(task, "Task State And Delegation", "Go", "Task synthesis, decomposition, delegation, specialist flow")
        Component(dispatch, "Dispatch And Context", "Go", "Skill-first, shortcut dispatch, retrieval, context building, cache prep")
        Component(infer, "Inference Orchestration", "Go", "Model selection, fallback inference, standard or streaming execution")
        Component(post, "Post-Turn Processing", "Go", "Guards, ingest, nickname refinement, trace, cost, persistence")
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

## 6. Supplementary Rule View — Streaming Is Not A Separate Product

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

## 7. Supplementary Rule View — No Symptom Fixes

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

## 8. Supplementary Rule View — Enforcement Model

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

## 9. Reading Guide

- Use the C4 context and container views to understand architectural ownership.
- Use the connector-layer component diagram when reviewing route, streaming,
  cron, channel, or CLI changes.
- Use the pipeline component diagram when deciding whether behavior belongs in
  the factory.
- Use the capability diagram when evaluating stage dependencies and service-bag
  creep.
- Use the supporting diagrams when validating streaming parity, debugging
  divergence, or explaining why a local connector patch is incorrect.

If a proposed code change does not fit cleanly onto these diagrams, the change
SHOULD be treated as architecturally suspect until its ownership becomes clear.
