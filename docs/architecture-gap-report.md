# Architecture Gap Report: Go Implementation vs Rust Reference

**Date**: 2026-04-19
**Auditor**: Automated deep audit (3 parallel agents)
**Scope**: Connector-factory compliance, security architecture, tool execution, context management, real-time transport, agentic retrieval architecture
**Reference**: `/Users/jmachen/code/roboticus-rust/ARCHITECTURE.md`

---

## Executive Summary

The Go implementation achieves **full structural compliance** with the connector-factory pattern. The pipeline is the single source of truth for business logic, all 8 entry points use `RunPipeline()`, and architecture tests enforce connector thinness. **All 7 original systemic gaps are now CLOSED** (v1.0.1 + v1.0.2 + v1.0.4), and the broader parity-forensics program has now been distilled into final validated or explicitly deferred dispositions rather than exploratory runtime-classification seams.

v1.0.5 introduced the **agentic retrieval architecture** scaffold — decomposer, router, reranker, context assembly, reflection, and working-memory persistence. v1.0.6 has now carried that scaffold much farther into runtime reality: router-selected retrieval modes influence actual tier retrieval, semantic / procedural / relationship / workflow reads are HybridSearch-first with per-tier `retrieval.path.*` trace attribution, semantic and relationship evidence preserve stronger provenance/freshness signals, the verifier consumes pipeline-computed task hints and claim-level proof obligations, a persisted graph-fact store now exists in production with reusable traversal APIs, and enriched episode distillation now promotes recurring canonical triples into `knowledge_facts`. v1.0.7 then closed the remaining architecture-led retrieval seams with explicit fusion, optional LLM reranking, and semantic FTS cleanup instead of residual heuristic SQL.

The parity-driven remediation effort also clarified several ownership seams that
older architecture docs had left too generic:

- **Request construction is now a first-class architecture seam.** Tool pruning,
  memory preparation, checkpoint restore, and prompt assembly converge into one
  `llm.Request`, and the request builder is now expected to preserve the latest
  user message, align prompt-layer tool guidance with the structured tool list,
  and drop empty compacted history before inference. Baseline/exercise now uses
  that same runtime request path rather than a direct-LLM bypass.
- **Low-level utility ownership is sharper.** Storage-layer repositories are not
  allowed to depend on `internal/agent/*` helpers for generic concerns like
  content hashing. Shared primitives such as hashing, normalization, and ID
  formatting must live at or below `internal/core`, or low-level dependency
  direction starts drifting upward.
- **Continuity and learning are now explicitly artifact-driven.** Reflection,
  executive growth, checkpoints, and consolidation are expected to consume
  structured turn artifacts (`tool_calls`, `pipeline_traces`,
  `model_selection_events`, structured `episodic_memory.content_json`) instead
  of re-deriving durable state from lossy text summaries.
- **Security/policy truth ownership is sharper.** Stage 8 owns claim
  composition, policy/tool runtime own what actually happened, and
  post-inference guards are no longer allowed to overwrite legitimate
  policy/sandbox denials with fabricated canned outcomes.
- **Webhook ingress ownership is sharper.** Telegram and WhatsApp routes no
  longer own transport JSON parsing; adapters own normalization and WhatsApp
  verification/signature checks, while routes only bridge normalized inbound
  messages into the pipeline.
- **Route-admin connectors must stay thin even when they expose many surfaces.**
  Admin policy, benchmark/exercise, and dataset export endpoints are not
  allowed to accrete into one giant route file that becomes a de facto service
  layer. Shared helpers may exist in `routes`, but the connector surface must
  remain split into bounded files so ownership still reads as connector logic
  instead of leaked business flow. Architecture enforcement must follow that
  connector surface rather than pinning the unified pipeline-path requirement
  to one historical filename after the split.
- **Verification coverage must derive one canonical subgoal set.** The verifier
  and executive-plan seam is not allowed to count both the unsplit whole prompt
  and the later conjunction-split parts as separate requested subgoals. Coverage,
  action-plan checks, unresolved-question growth, and retry guidance must all
  consume one canonical set or the system creates fake `2/3` failures from its
  own parser instead of the model response.
- **Verification must distinguish task content from output-shape directives.**
  Clauses such as `return only the number`, `reply on one line`, or other
  response-shape instructions are not independent semantic subgoals. The prompt
  contract must parse those once and feed the same normalized directive set into
  verifier coverage, direct-response shaping, and retry guidance so the system
  does not manufacture false subgoal failures from formatting instructions.
- **Formatting-only directives must not become durable executive state.**
  Output-shape setup prompts such as `reply only with noted` or `return only
  the number` are not unresolved business obligations. The same canonical
  formatting-directive normalization seam must feed executive growth so those
  directives never open unresolved questions that later poison continuity
  verification.
- **Continuity turns must treat session history as first-class evidence.**
  Questions like `what codename did I tell you to remember` or alias-driven
  follow-ups such as `create the quiet ticker` are not supposed to depend on
  generic memory search artifacts or `no evidence found` gaps when the required
  facts already exist in durable session history. Verification and retrieval
  policy must share one continuity contract for that seam.
- **Continuity evidence outranks generic retrieval gaps.**
  When the verifier has canonical continuity evidence from the current session,
  stale `[Gaps]` markers from unrelated retrieval are not allowed to force
  degraded uncertainty language on otherwise well-supported recall answers.
- **Filesystem allowlists must compare canonical path identity, not raw string
  case.** Absolute-path authorization is not allowed to reject a real allowed
  path just because one layer sees a symlinked alias such as `/var/...` while
  another sees the resolved root such as `/private/var/...`. That rule also
  applies to future descendants beneath an allowed/workspace root; non-existent
  child write targets must inherit the canonical identity of their nearest
  existing ancestor so write authorization and read authorization cannot drift.
  path simply because one layer used `/Users/...` and another stored
  `/users/...`. Sandbox truth must canonicalize path identity once and reuse it
  across tool resolution, policy checks, and proof-of-work.
- **Tool-bearing routing must use tool-use evidence as the primary capability
  authority.** When a request carries tools, routing is not allowed to keep
  selecting merely generic or under-evidenced candidates while observed
  `TOOL_USE` candidates exist. Recommendation language and live selection must
  agree on which models are actually being ignored.
- **Rebuildable derived storage must repair centrally or fail loudly.**
  Corruption confined to non-authoritative observability tables
  (`pipeline_traces`, `turn_diagnostic_events`, dependent trace indexes) or
  rebuildable FTS internals is not allowed to poison the authoritative
  conversational state or force caller-local hacks. The database seam must own
  corruption detection, scoped repair, FTS backfill, and one retry of the
  original operation. If corruption extends beyond rebuildable derived
  structures, boot or write paths must fail loudly instead of pretending the
  store is trustworthy.
- **Filesystem inspection turns are their own focused execution class.**
  Imperative count/list/find/scan turns over files or directories are not
  allowed to rely on semantically lucky default pruning or arrive with only
  memory/introspection tools. The turn envelope and tool-pruning seams must
  share one focused inspection profile that pins the minimal authoritative
  filesystem inspection surface (`glob_files`, `list_directory`, `read_file`,
  runtime context) for direct inspection work. That same authority must also
  resolve common operator path aliases such as `~`, “home folder”, and other
  allowed-root shorthands instead of silently downgrading them into
  conversational turns.
- **Focused inspection turns require structured evidence, not text-only output.**
  `glob_files`, `list_directory`, and other bounded inspection tools are not
  allowed to leave the loop, verifier, and RCA guessing from free-form text
  about whether a read-only call actually narrowed the task. Inspection tools
  must emit one shared typed evidence artifact (scope, count, emptiness) so
  direct-execution inspection turns can distinguish useful progress from
  dead-end exploration on one central seam.
- **Focused scheduling turns must separate known procedure from continuity.**
  Creating or listing cron jobs is not inherently “procedurally uncertain”
  once the focused scheduling surface exists. Explicit scheduling requests must
  stay on the focused scheduling envelope without widening into
  applied-learning retrieval, while session-defined aliases such as “quiet
  ticker” must be treated as continuity retrieval problems so shorthand can be
  resolved from prior session context instead of being guessed or ignored.
- **Inspection-shaped questions must share the same focused-inspection
  authority as imperative inspection turns.** Questions like `what's in the
  vault`, `show me the files`, or `what about the vault in your workspace`
  cannot be allowed to fall back to generic `question` handling while `count`
  / `list` / `find` variants get the focused inspection envelope. Task
  synthesis, retrieval gating, and turn-envelope selection must reuse one
  inspection-turn detector so phrasing does not create divergent execution
  policy. That shared detector must also cover path-shaped inspection requests
  such as `brief summary of the contents of /path/...` or `list the projects in
  /path/...`, not just imperative verbs.
- **Focused inspection turns must resolve targets through one shared runtime
  seam.** Inspection turns are not allowed to leave path resolution entirely to
  prompt-time model reasoning when runtime policy already exposes enough truth
  to resolve the target. The pipeline/session seam must resolve explicit paths
  and obvious allowlisted aliases (for example a single Desktop vault) once and
  pass that result into the prompt/runtime context. If the target is still
  ambiguous after that resolution pass, the model must ask a precise
  clarification question instead of claiming incapacity or inventing a denial.
  Path-clarification follow-ups such as `the vault in question is at /path/...`
  are part of that same seam; they are not allowed to fall back to generic
  question handling once the operator has supplied the concrete target.
  Alias-driven inventory questions such as `what are the most recently updated
  projects in my code folder` are also part of that seam; folder aliases like
  `code folder` must resolve onto the same focused inspection path instead of
  widening into generic question/retrieval behavior.
- **Filesystem destination resolution must use that same authority for authoring
  turns.** If the operator asks to write into an allowlisted non-workspace
  destination such as the Desktop Obsidian vault, the runtime is not allowed to
  leave destination resolution to prompt-side guesswork or claim that the path
  is unwritable while inspection of the same target is already permitted. The
  pipeline/session seam must resolve explicit destination paths and obvious
  authoring aliases (for example `my obsidian vault on my desktop`) once, pass
  that resolved destination into the prompt/runtime context, and preserve the
  distinction between the configured default vault (`obsidian_write`) and other
  allowlisted vaults or folders (`write_file` with an absolute allowed path).
- **Inspection-backed report authoring is task execution, not creative writing.**
  If the operator asks for a report or inventory derived from a concrete
  filesystem target and also asks that report to be written as an artifact, the
  turn is not allowed to classify as generic `creative` work or widen onto the
  default broad tool surface. Task synthesis, retrieval gating, and
  turn-envelope policy must recognize this class once, keep it on a bounded
  analysis+authoring path, and preserve the authoritative inspection target and
  destination target through the same shared seams. Within that bounded path,
  directory-wide metadata reports must prefer structured inventory / shell
  evidence over extension-sampling heuristics, and they are not allowed to
  write placeholder-heavy partial artifacts before the requested fields have
  been gathered or explicitly proven unavailable.
- **Filesystem write tool contracts must describe the real write surface.**
  `write_file` and `edit_file` are not allowed to describe themselves as
  workspace-only when the runtime can legally write to absolute allowlisted
  destinations. Prompt/tool metadata must expose the real confinement rule or
  the model will manufacture false access denials on valid authoring requests.
- **Release-shaped binaries must derive version truth from one build seam.**
  CI, release packaging, and local release-helper builds are not allowed to
  stamp different or nonexistent CLI version symbols. `roboticus version` must
  read from the same injected `cmd/internal/cmdutil.Version` symbol that the
  release workflow stamps, while the daemon banner continues to read from
  `internal/daemon.version`. A release-shaped binary that still reports `dev`
  is a deployment-truth defect, not harmless metadata drift.
- **Release-gate hygiene must stay behavior-neutral.**
  Dead helper seams, unused parser remnants, and mechanical staticcheck drift
  are not allowed to accumulate on the release branch. Lint cleanup at this
  stage must remove unused or misleading code without inventing new control
  flow or changing the authoritative runtime behavior.
- **Release workflows must not depend on deprecated GitHub Action runtimes.**
  Active CI and release workflows are not allowed to keep Node 20-based action
  majors or composite actions that vendor stale Node 20 dependencies into the
  release gate. Workflow hygiene is part of release truth; noisy deprecation
  warnings must be removed by upgrading or inlining those action paths before
  the ceremony is considered clean.
- **Release security gates must run on the same patched toolchain the repo
  declares.** CI security checks are not allowed to rely on a vulnerable Go
  patch line while local development silently passes on a newer auto-selected
  toolchain. The module-declared Go version is part of release truth and must
  be raised immediately when the active CI standard-library line is flagged by
  `govulncheck`.
- **MCP stdio diagnostics must wait on collector completion, not buffer
  heuristics.** Startup failure reporting is not allowed to poll stderr-buffer
  length and infer that the final diagnostic tail has landed. The transport
  must own an explicit stderr-collector completion signal so slower CI runners
  cannot lose the actionable tail of a real child-process failure.
- **Formatter fuzz paths must remain linear on malformed delimiter floods.**
  Channel formatters are not allowed to repeatedly rescan the remainder of the
  input for every unmatched inline marker, because release-gate fuzzing will
  turn that into timeout failures on slower CI runners. Markdown-to-channel
  transforms must keep malformed or adversarial delimiter handling bounded and
  deterministic instead of relying on quadratic forward scans.
- **Focused profiles must derive from one complete tool-semantics map.** A
  bounded turn policy is not allowed to silently drop a legitimate inspection or
  read tool because that tool was never classified in the central semantics map.
  Tool-profile policy, replay rules, and RCA interpretation must all consume
  one authoritative classification table that includes the real inspection
  surface (`glob_files`, `list_directory`, `search_files`, `read_file`, and any
  other first-class inspection tools).
- **Large project-root inventory is a first-class inspection capability.**
  When the operator asks for a report over a code/projects root with fields like
  project name, language, timestamps, or git direction, the system is not
  allowed to rely solely on fragile multi-step `bash` choreography or
  extension-globbing heuristics. The tool layer must own one authoritative
  project-inventory capability that can inspect an allowed root, enumerate
  candidate project directories, derive bounded metadata, and feed that result
  into the same focused analysis+authoring path as other inspection evidence.
- **Tool-call exchanges are atomic under request compaction.** When budget
  pressure forces history trimming, the context builder must treat an assistant
  tool-call message and its corresponding tool-result message(s) as one atomic
  exchange. It is not allowed to keep the tool-call while dropping the tool
  response, because that corrupts provider requests and turns successful
  tool-backed work into fallback noise. The same rule applies to the
  pipeline-owned pre-inference compactor that mutates live session history
  before inference begins; it is not allowed to orphan a historical
  `tool_call_id` and poison a later verifier retry or follow-up inference.
- **Tool execution context must carry the live runtime dependencies that the
  selected tools actually require.** Database-backed tools are not allowed to
  pass in isolated unit tests and then fail in the live loop with `database
  store not available` because the loop forgot to thread the authoritative
  store into `ToolContext`.
- **Artifact-backed completion may satisfy response coverage for authoring
  turns.** When the operator asked for a report/document/file to be written and
  the system has authoritative proof that the artifact was created in the
  requested destination, the final assistant reply may be a concise completion
  confirmation. The verifier is not allowed to degrade that turn merely because
  the reply does not restate every internal field that belongs inside the
  authored artifact itself. Chat-level subgoal coverage is not allowed to
  override authoritative write-boundary truth on a successful artifact-backed
  authoring turn. This exemption applies only to artifact-internal requested
  parts; mixed-output turns that also ask for an in-chat summary, explanation,
  recommendation, or other operator-facing output must still satisfy chat-level
  coverage for those non-artifact parts.
- **Advisory liveness warnings are not terminal degradation.** A transient
  watchdog event such as `stage_liveness_warning` may remain RCA-visible, but it
  is not allowed to mark an otherwise successful turn as `degraded`. Terminal
  turn status must come from failure, retry, or contradiction seams that still
  matter at finalization, not from advisory timing probes that ultimately
  resolved.
- **Artifact overclaim verification applies to authored outputs, not inspection
  evidence reporting.** A read-only inspection turn is allowed to mention file
  and directory names discovered through authoritative inspection/read tools
  without being treated as if it claimed to create those artifacts. The
  verifier must gate `artifact_set_overclaim` on output-contract / artifact-
  mutation turns, not on any response that happens to contain filenames.
- **Direct execution turns must not terminate on promissory filler.**
  Responses such as `let me check`, `I'll inspect that`, or equivalent
  future-action scaffolding are not completed work on an `execute_directly`
  turn. The same direct-execution boundary that rejects placeholder-only output
  must also reject promissory no-tool replies unless the turn already carries
  authoritative evidence that the requested action has been completed.
- **Filesystem tool contracts must describe the real sandbox, not a narrower
  fiction.** If read/list/glob tools can operate on workspace-relative paths
  and absolute paths inside `allowed_paths`, their schema and descriptions must
  say that explicitly. A workspace-only description biases the model into the
  wrong path form and turns an allowed folder into a false execution failure.
- **Runtime-context reporting must match effective sandbox truth.**
  If prompts tell the agent to consult `get_runtime_context` for path/security
  policy, that tool must return the effective confinement rules the tools
  actually enforce: workspace anchoring for relative paths, absolute-path
  constraints, and any protected read-only inputs for the turn. The runtime
  tool is not allowed to claim “security policy” while only exposing a partial
  path list.
- **Scheduling turns are their own focused execution class.** A direct request
  to create or inspect a cron/scheduled job is not supposed to widen into a
  general-purpose 15-tool exploratory surface. Scheduling turns must pin the
  authoritative scheduling tool and only the minimum supporting tools needed for
  execution proof.
- **Explicit acknowledgement directives belong on the shortcut seam.** When the
  user asks for a one-sentence acknowledgement and wait state, the system is not
  supposed to improvise a broader conversational reply. The shortcut layer must
  own that bounded response class instead of treating it as open-ended model
  prose.
- **Plugin runtime ownership is sharper.** Daemon startup now owns plugin
  registry construction, directory scan, manifest parsing, init, and
  `AppState.Plugins` wiring. Install-time plugin writes now hot-load into that
  same registry, so plugin install/catalog UX no longer stands in for a missing
  runtime lifecycle. Manifest-backed plugin scripts now also share the same
  core execution contract as skill scripts, closing a policy drift seam at the
  extension boundary.
- **Manual cron execution now shares the durable scheduler lifecycle.** The
  live `/api/cron/{id}/run` path no longer bypasses lease/run-history/retry
  ownership; it delegates through `CronWorker.RunJobNow(...)` and preserves the
  same execution contract as scheduled runs.
- **Model lifecycle policy is now a first-class routing seam.** Live routing is
  no longer allowed to rely purely on metascore and fallback order to avoid bad
  candidates by accident. Per-model lifecycle state (`enabled`, `niche`,
  `disabled`, `benchmark_only`) and role eligibility (`orchestrator`,
  `subagent`) are now explicit, operator-visible, reasoned, and evidenced
  before ranking starts.
- **Operational delegation inventory belongs on the runtime tool surface.**
  Subagent roster and skill inventory are no longer allowed to exist only as
  admin/UI introspection or prompt-side capability snapshots. The live
  tool-pruning/request path must own explicit roster/inventory tools so
  orchestrators can inspect delegation capacity through the same authoritative
  surface they use for other operational decisions.
- **Subagent composition now belongs on the same runtime control plane.**
  Creating or updating subagents is no longer allowed to remain an admin-only
  route concern. The live tool surface now owns first-class subagent
  composition through one authoritative repository, with explicit
  orchestrator-only enforcement.
- **Delegated task lifecycle now belongs on the same runtime control plane.**
  Open delegated work, retry requests, and task-level status inspection are no
  longer allowed to remain embedded in connector-local routes, status sidecars,
  or orchestration-only in-memory structures. The runtime tool surface now owns
  first-class task lifecycle tools backed by one authoritative repository over
  delegated task state and events.
- **Multi-subagent orchestration now uses that same delegated-work control plane.**
  The system no longer treats orchestration as a prompt-only delegation trick
  layered over the loop. Workflow creation, assignment, and lifecycle evidence
  now flow through one authoritative orchestration control surface that writes
  the same `tasks`, `task_events`, and `agent_delegation_outcomes` artifacts
  the runtime already exposes for task inspection and retry.
- **Operator RCA flow is now a canonical diagnostics surface, not a trace dump.**
  The WebUI observability surface is expected to consume `turn_diagnostics`
  summary + events as the authoritative RCA artifact and render them as one
  canonical decision flow. The operator contract is macro-by-default,
  detailed-on-demand, and grouped by task / envelope / routing / execution /
  recovery / outcome seams instead of raw event order. That contract is not
  allowed to disappear when a turn predates canonical diagnostics or the
  diagnostics artifact is missing: the flow surface must keep visible
  macro/detail controls and fall back explicitly to a trace-only narrative
  keyed by the turn id instead of silently collapsing back into the old stage
  dump. The desktop presentation contract is left-to-right and bounded, but
  more importantly it is singular: there is one decision flow, not a stage
  strip beside a second RCA rail. Macro mode uses compact flow blocks only,
  with dense status state consolidated into one narrow top banner instead of a
  row of large tiles. Turn conclusion and health rating should not compete
  with that strip; they may live in a separate bottom banner when space is
  tight. That top banner must use the same severity model as the flow itself:
  degraded status is yellow, latency above one second is yellow, latency above
  one minute is red, and `high` / `critical` pressure is red. The top-line
  `Health` value is not allowed to read like an unrelated hidden score; it
  must be explicitly derived from the aggregate of the category outcomes shown
  in the flow. Those banner chips are also not allowed to rely on insider
  shorthand alone: each top-line metric must expose an explanatory hover/focus
  tooltip so values such as `degraded`, `high`, or `swap 78.8%` can be
  interpreted by an operator without leaving the flow or querying logs. Visible
  node copy must stay utility-first: macro nodes show only the
  single most decision-relevant signal for that node, with duration as the
  default and routing as the explicit exception because the selected model is
  more important than repeating stage identity. Verbose explanation belongs in
  a true floating tooltip layer anchored to pointer/focus position or in
  explicit detail mode rather than permanently expanded text panels. Detail
  mode must preserve ordinality first: operators should read one chronological
  event timeline, with task / envelope / routing / execution / recovery /
  outcome shown as annotations, not as separate buckets that force manual
  reconstruction of sequence. The flow
  container itself must remain
  width-bounded to the usable main-pane area, accounting for persistent chrome
  such as the sidebar, and that bound must come from the real content pane
  rather than raw viewport math. If the decision rail outgrows that space, the
  surface must expose intentional horizontal scrolling contained inside that
  pane. The flow must also preserve causal atomicity for repeat execution:
  when any step runs more than once, the affected block must carry an explicit
  repeat marker and the operator must be able to infer, from the UI alone,
  whether an earlier attempt succeeded, which guard or verifier intervened
  afterward, the exact retry reason, whether the retry reused the same
  model/provider or widened to fallback, and the final outcome. Stale
  trace-only fallback overlays are not allowed to survive session changes,
  expanded-row changes, or collapse/expand transitions once canonical
  diagnostics are available. The conclusion banner is not allowed to merely
  acknowledge that diagnostics exist; it must synthesize what the evidence
  implies about the turn. For degraded or retried turns, that means naming the
  causal event that changed the path (for example a post-success guard retry),
  whether the route widened or stayed the same, and what that implies about the
  likely fault boundary. Flow nodes themselves should also encode outcome
  quality visually: green for clean execution, yellow for concerns or partial
  degradation, and red for broken or clearly failed paths. Those colors must be
  derived from the same persisted RCA evidence rather than ad hoc UI guesses.
- **Trace and diagnostics artifacts must share the same turn identity.**
  Observability is not allowed to infer RCA presence by heuristic timestamp or
  session adjacency. `pipeline_traces.turn_id` and `turn_diagnostics.turn_id`
  must be written from the same authoritative turn record ID on live turns, or
  the operator flow will misclassify fresh canonical-diagnostics turns as
  trace-only fallback.
- **Host resource state is now part of benchmark validity and RCA truth.**
  Baseline runs, prompt-level exercise rows, and live turn diagnostics are not
  allowed to omit the machine state they were executed under. CPU, memory,
  swap, and relevant process RSS snapshots must be captured on the same
  central seams that already own benchmark persistence and inference RCA,
  otherwise the system cannot distinguish a weak model from a saturated host.
- **Benchmark validity also requires model/runtime-state evidence.**
  Benchmark persistence is not allowed to record a zero-content or failed
  exercise row without also recording the execution preconditions for the
  specific model under test. At minimum, the persisted benchmark artifact must
  capture provider/model runtime state on the same start/end seams that already
  own host-resource snapshots, so operators can distinguish "the model
  responded badly" from "the model was missing, unloaded, unreachable, or
  otherwise not actually ready to serve the request."
- **Explicit session IDs must resolve to durable session truth or fail cleanly.**
  Session resolution is not allowed to fabricate an in-memory session shell
  for an explicit `session_id` that does not exist in `sessions` and then let
  message persistence trip a foreign-key constraint later. When a caller
  supplies a body-scoped session id, the pipeline must either prove that the
  session exists or reject the request as not found before any turn/message
  writes occur. A storage-time `500` here is a session-authority defect, not a
  valid runtime outcome.
- **Provider identity must survive routing target formatting.**
  Routing is not allowed to select a target on one provider and then erase that
  provider identity when the chosen target is formatted into a request model
  spec. Provider-qualified downstream namespaces such as
  `openrouter/openai/gpt-4o-mini` must preserve the outer execution provider
  (`openrouter`) instead of being reinterpreted later as a direct `openai`
  request. Otherwise the request path silently blames the wrong provider and
  turns configuration truth into spurious auth failures.
- **Successful side-effecting tool calls must be replay-protected inside a
  turn.**
  Once a tool that mutates the world has succeeded, the framework is not
  allowed to execute the same side-effecting call again in the same turn
  unless that tool explicitly declares replay safety. For note authoring,
  shell execution, authority mutation, delegation, or any other persistent
  effect, duplicate execution is a correctness risk rather than a mere latency
  concern. Replay protection must be decided from the same authoritative tool
  semantics map used by turn shaping, not from ad hoc name checks in the loop.
  Replay identity is also not allowed to collapse to raw argument equality:
  side-effecting tools must resolve one semantics-derived protected
  resource/effect fingerprint, with typed artifact proof taking precedence
  after a successful write. That protection is not allowed to remain an
  invisible loop detail: canonical diagnostics and operator RCA must expose
  replay suppression count, the protected tool/resource, and the suppression
  reason from the same persisted execution fact.
- **Procedural uncertainty should be allowed to pull applied memory before the
  model starts exploring.**
  Retrieval gating is not allowed to consider only continuity, evidence, and
  risk while ignoring “we may already know how to do this.” When the framework
  is procedurally uncertain about task execution, it should be able to consult
  learned workflows and prior outcome evidence — including known failures —
  before the model begins exploratory thrashing. That decision belongs on the
  same task-synthesis / perception seam that already owns intent,
  decomposition, source-of-truth, and retrieval policy so RCA can explain why
  applied-learning retrieval was or was not used. That explanation is not
  allowed to live only in trace annotations: canonical diagnostics must record
  a chronological event describing whether the framework looked for prior
  successes, prior failures, or both before the first inference call.
- **Current-turn retrieved evidence is the first memory authority.**
  Once the pipeline has already assembled `[Retrieved Evidence]`, `[Gaps]`, and
  the memory index for the current turn, the prompt is not allowed to tell the
  model to re-search memory unconditionally. Injected current-turn evidence must
  be treated as the first memory authority for that turn, and follow-up
  `recall_memory` / `search_memories` calls are only justified when that
  injected evidence or index is actually insufficient for the specific task.
  Otherwise the framework teaches the model to rediscover facts it already
  provided, inflates tool-bearing turns, and creates exploratory memory churn
  that RCA later has to explain as if it were model behavior instead of prompt
  contradiction.
- **Bounded multi-artifact authoring is still focused direct work.**
  A turn that asks for a small, explicit set of concrete artifacts — for
  example two or three notes/files with exact content and linking — is not
  allowed to fall out of the focused authoring path merely because it is no
  longer literally “single-step.” When the work is still direct, bounded, and
  artifact-shaped, task synthesis and envelope policy must keep it on a focused
  execution surface with artifact-proof requirements instead of classifying it
  as `complex` specialist work and dragging a heavy generic tool surface back
  into the request.
- **Source-backed authoring needs first-class source-read semantics.**
  When a turn asks the system to produce artifacts from an existing source file
  or other prompt-named input artifact, the focused authoring envelope is not
  allowed to treat authoritative source reads as an unknown or optional
  side-path. `read_file`-style source reads must live in the same central tool
  semantics map and focused-authoring tool policy as artifact writes, because
  the framework cannot close `source_artifact_unread` gaps honestly if the
  corrective path does not preserve source-read capability on the turn surface.
- **Source-backed exact artifact authoring must stay on the authoring path.**
  A turn that says “read this source artifact, then create these exact output
  artifacts” is not allowed to be upcast into the generic `code` / heavy /
  delegation path merely because it mentions JSON, runbooks, or workflow-like
  nouns. If the prompt already defines a bounded expected-artifact contract,
  task synthesis must keep the turn in the direct authoring family so the
  focused authoring tool surface and retry policy can do their job.
- **Verifier retries must translate source-proof gaps into corrective action.**
  A verifier finding such as `source_artifact_unread` is not allowed to remain
  a generic “revise your answer” prompt. The retry seam must own a structured
  corrective plan that tells the model what evidence to gather next, and when
  the missing proof is a prompt-named source artifact the corrective path must
  prefer authoritative source reads over memory-search churn. Otherwise the
  system knows what is wrong without changing the control path that caused it.
- **The selected tool surface is the only authority for prompt and execution.**
  Once the pipeline selects the bounded tool surface for a turn, both the
  prompt-layer tool guidance and the execution loop must obey that same set. A
  tool that is not on the selected surface is not allowed to stay named in
  generic prompt instructions, and an out-of-surface tool call is not allowed
  to execute merely because the global registry knows the tool name.
- **Artifact names are not authority-layer mutation directives.**
  A request to create or update a file/note named `runbook`, `policy`, `spec`,
  or similar is not allowed to flip the turn into authority-mutation mode by
  itself. Authority-layer mutation must be triggered by explicit persistence or
  registry semantics, not by generic file-authoring verbs plus artifact names.
- **Prompt-declared source artifacts are read-only turn resources.**
  If the prompt names an input artifact to read from, that source path is not
  allowed to remain a soft verifier hint only. The execution seam must carry it
  as a protected read-only resource for the current turn, and artifact-writing
  tools must reject attempts to overwrite that source path while satisfying the
  same request.
- **Source-backed authoring must pin an authoritative source-read tool.**
  If the prompt declares source artifacts and also asks for direct artifact
  creation or update, the focused tool surface must include an authoritative
  file-read tool rather than relying on semantic ranking to maybe discover one.
  `source_artifact_unread` is execution-critical, not a narrative-only
  afterthought, so the selection seam and retry policy must both preserve that
  fact.
- **Direct execution turns must not degrade into successful read-only research
  loops.**
  Once task synthesis says the turn should `execute_directly`, the loop is not
  allowed to keep spending successful runtime-context, workspace-inspection,
  capability-inventory, task-inspection, or memory-read calls forever while no
  artifact write, execution step, delegation step, or other real progress ever
  occurs. A small amount of exploration is legitimate; repeated successful
  read-only exploration without execution progress is framework-owned churn and
  must terminate on a central semantics-driven rule rather than a tool-name
  blacklist. Canonical diagnostics must expose the blocked tool, the
  exploration streak count, and the `exploratory_tool_churn` termination cause
  so operators can see that the framework stopped itself instead of blaming the
  model for an infinite research loop.
- **Exact-content artifact proof must originate once at the write boundary and
  survive into verification unchanged.**
  File/note/document-writing tools are not allowed to collapse successful
  artifact creation into a byte-count string and force guards, verifier, RCA,
  or the model itself to reverse-engineer what was written from lossy text. The
  write boundary must emit one typed artifact-proof payload containing at least
  artifact kind, path, bytes, content hash, and exact-content evidence when the
  write is small enough to preserve it safely. Session history must preserve
  that proof as typed tool-result metadata, and guard/verifier context must
  project that same proof forward instead of reparsing ad hoc strings. On
  direct authoring turns, successful artifact proof is the primary
  post-execution evidence for certainty checks; stale pre-inference retrieval
  gaps or contradictions are not allowed to outweigh proven artifact writes and
  force a degraded “cannot guarantee integrity” answer after the system already
  has exact write evidence.
- **Expected exact artifact specs must also be parsed once and reused across
  turn control, decomposition, and verification.**
  When a prompt explicitly names one to three artifacts and says they should
  contain exact content — including equivalent directive forms such as
  `containing exactly` and `with content` — that expectation is not allowed to
  be rediscovered by separate lexical heuristics in task synthesis,
  decomposition, turn-envelope policy, guard policy, and verifier logic. The
  pipeline must parse one bounded expected-artifact spec artifact from the
  prompt, resolve relative artifact names against any explicit container
  directory named in that same prompt, reuse that same typed expectation to
  keep the turn on the focused authoring path, prevent embedded artifact bodies
  from inflating decomposition, cut exact artifact bodies off before trailing
  post-artifact follow-up directives, compare it directly against typed write
  proof, and re-verify any verifier-triggered rewrite before finalization.
  Exact-content mismatch is an execution-critical failure, not a narrative
  concern, and must remain retry-blocking even after successful tool-backed
  progress.
- **Artifact verification must be symmetric across required, written, and
  claimed file sets.**
  The same authority that proves required artifacts were written is not allowed
  to ignore invented file claims in the answer or unexpected extra writes at
  the tool boundary. If the response names `gamma.txt`, or the runtime writes
  `gamma.txt`, but the exact requested artifact set only contains `alpha.txt`
  and `beta.txt`, verification must fail as an artifact-set violation instead
  of passing because the required files happened to conform.
- **Prompt-boundary artifact parsing must classify source/input artifacts
  separately from expected outputs.**
  If a turn says “read `requirements.txt`, then create `deploy-config.json` and
  `rollout-runbook.md`”, verifier claim checks are allowed to treat
  `requirements.txt` as evidence provenance, but they are not allowed to flag a
  truthful reference to that source file as an invented output artifact claim.
- **Novel procedural experience must be captured with future reuse in mind.**
  Once the framework learns that a workflow succeeded, failed, or only
  partially worked, that outcome is not allowed to remain buried in one-off
  conversational text. Distillation/promotion must preserve reusable
  procedural semantics, outcome polarity, and enough task-shape context for
  later applied-learning retrieval to ask not only “have we done this before”
  but also “did it work.” RCA must be able to show when a novel experience was
  judged reusable, which outcome polarity was captured, and whether the system
  merely stored an episodic lesson or promoted reusable procedural knowledge
  for future turns. Final turn disposition and verifier result must remain
  authoritative for that polarity; a degraded or verifier-failed turn is not
  allowed to be captured as `success` just because the underlying tool calls
  happened to succeed. Those facts are not allowed to remain detail-only side
  notes: the canonical RCA summary and the operator flow must treat
  pre-inference applied-learning and post-turn reuse capture as first-class
  causal facts in the same decision narrative as task, routing, execution,
  recovery, and outcome.
- **Structured tool I/O must pass through one normalization authority.**
  That authority is not allowed to stop at argument salvage inside the agent
  loop. Provider-facing tool message serialization and provider-returned tool
  message normalization must also be owned centrally, so formats like
  Ollama's documented `role=tool` + `tool_name` contract cannot drift behind
  an OpenAI-compatible fallback. The same canonical seam must be able to say
  `no_transform_needed`, `qualified_transform_applied`,
  `transform_failed`, or `no_qualified_transformer`, and those outcomes must
  be visible in RCA. Raw provider request/response envelopes for benchmark and
  RCA analysis must be captured from that same seam rather than reconstructed
  later from scored content alone.
  The framework is not allowed to hand malformed provider-emitted tool
  arguments straight to builtin/plugin/MCP tools and hope each tool invents
  its own repair logic. Tool-call arguments must cross one ordered
  normalization factory before policy evaluation and execution, and tool
  results must cross that same normalization authority before they are written
  back into session history or fed to the model. The zero-transform case is a
  valid outcome of that same pipeline, not a parallel bypass. Normalization is
  also not allowed to become a graveyard of silent one-off hacks: any repair,
  fidelity loss, or hard failure must be emitted as canonical RCA evidence so
  operators can see whether the framework repaired malformed tool I/O or
  rejected it honestly. The factory must also treat lack of a qualified
  transformer as a first-class outcome, because “we saw a malformed shape but
  did not have a safe transformer for it” is operationally different from both
  “no transform needed” and “repair failed.” Post-v1.0.7, this seam may be
  worth opening to data-driven or externally supplied normalizer definitions,
  but only after the core normalization contract, RCA visibility, and safety
  boundaries are proven under one in-process authority first.
- **Historical tool-call truth must not share mutable state with pending
  execution state.**
  The same tool-call set is used for two different purposes inside a turn:
  preserving what the model actually requested in conversation history and
  tracking which calls are still pending execution. Those are not allowed to
  share one mutable backing slice or the framework will silently rewrite the
  historical assistant message while resolving tool results. That poisons raw
  provider follow-up requests, canonical RCA envelopes, and operator trust
  because the system appears to have sent a different tool-call plan than the
  model actually emitted. Session history must preserve the original assistant
  tool-call set immutably, while pending execution state must be a separate
  mutable projection.
- **Agent roster surfaces must share one authoritative subagent projection.**
  The roster view and the editable subagent list are not allowed to drift by
  querying different route-local shapes over the same `sub_agents` corpus. The
  enriched roster projection is now the shared read model, with the roster page
  layering the orchestrator card on top only where that page actually needs it.
- **Skill composition now has to use one shared runtime control plane as well.**
  Creating or updating skills is not allowed to remain a route-only file-write
  helper. The live runtime tool surface, admin/catalog install flow, and skill
  inventory must converge on one authoritative repository that owns both the
  on-disk skill artifact and the `skills` table row.
- **Simple direct tasks must not be widened into heavy autonomous turns by intent alone.**
  The envelope owner is no longer allowed to treat every `task` intent as
  `heavy` regardless of complexity and planned action. When task synthesis says
  `simple` + `execute_directly`, the first-pass request must stay on a focused
  execution envelope with bounded context, bounded tool surface, and
  retrieval only when concrete continuity or evidence signals require it.
  “Task” is too broad a bucket to justify full autonomous tool-bearing ReAct
  behavior by itself.
- **Retrieval for action turns must be evidence-based, not intent-defaulted.**
  Task synthesis and retrieval policy are not allowed to infer
  `retrieval_needed = true` merely because a turn is imperative. Direct
  authoring or file-manipulation requests that do not depend on prior state,
  historical context, or canonically retrieved evidence must be able to stay
  local to the workspace/tool surface. Otherwise the system manufactures
  pressure and autonomy for no gain.
- **Workspace-local vault authoring must be a first-class runtime capability.**
  Obsidian integration is not allowed to remain a prompt hint or an indirectly
  referenced skill. If a vault is configured and sits within the runtime's
  writable workspace/allowlist, the live tool surface must expose an explicit
  vault-authoring capability with semantics aligned to note creation/update.
  Operators should not have to rely on the model inferring that a generic file
  tool plus a prose hint imply safe vault authoring.
- **Capability truth must converge before inference.**
  Task synthesis, skill inventory, runtime skill loading, tool registration,
  prompt guidance, and operator UI are not allowed to maintain separate partial
  truths about what the agent can do. If an enabled skill exists in the
  authoritative inventory, the runtime must either load it into the live
  matcher/tool surface or mark it unavailable for a concrete reason that every
  other layer can see. DB-backed skill catalogs, filesystem-backed runtime
  matchers, and config-gated tool registration must not drift independently.
- **Guard-context temporal atomicity must hold.**
  Cross-turn guards are not allowed to compare a completion against assistant
  content already emitted inside the same turn. `PreviousAssistant` and
  `PriorAssistantMessages` must exclude the in-flight turn's assistant output
  while still preserving current-turn tool results for truth/execution guards.
  Otherwise successful tool-backed confirmations are misclassified as
  repetition and the framework manufactures pointless retry churn.
- **Skill/capability matching must be semantic enough to preserve operator intent.**
  Capability fit is not allowed to be derived from whitespace-splitting raw
  skill names while ignoring enabled skill descriptions, triggers, aliases, or
  punctuation boundaries. Otherwise the system will conclude that `obsidian`
  and `vault` are missing even when `obsidian-vault` is installed and enabled.
- **Intent classification must not widen simple authoring turns on lexical noise.**
  The first-pass task classifier is not allowed to treat generic words like
  `test` inside filenames, titles, or note bodies as sufficient evidence for a
  coding turn. Simple document/note authoring requests must stay on the direct
  execution path unless stronger coding evidence exists.
- **Complexity classification must not upcast single-step direct authoring on word count alone.**
  A verbose but still single-step authoring/editing request is not allowed to
  become `moderate` or `complex` merely because the operator specified output
  constraints in full sentences. For direct note/document/file authoring, the
  classifier must weight structural step count and artifact count ahead of raw
  length, or the focused execution envelope can never activate on realistic
  operator requests.
- **Focused authoring turns must use a capability-scoped tool profile.**
  Once a turn is classified as simple direct note/document/file authoring, the
  focused envelope is not allowed to inherit the generic operational
  always-include set. The selected tool surface must be shaped by the tool's
  operation class: artifact-writing tools first, minimal runtime/workspace
  context second, retrieval only when continuity evidence exists, and no
  delegation, authority mutation, or unrelated operational inventory by
  default. Otherwise the framework remains formally "focused" while still
  shipping broad ambient capability baggage into the request.
- **Post-success retries must be adjudicated by execution progress, not style alone.**
  Once a turn has already made substantive execution progress — for example a
  successful artifact write or other successful tool-backed action — the guard
  and verifier layers are not allowed to trigger another full inference attempt
  for purely narrative-quality defects such as repetition or unsupported
  certainty. Those concerns still belong in RCA, but retry authority after
  progress must be reserved for execution-critical failures: false execution
  claims, materially incomplete work, broken output contracts, unmet stopping
  criteria, or other defects that make the claimed result untrustworthy.
- **Lifecycle policy must influence routing, not just candidate admission.**
  `niche` and `under_scrutiny` are not allowed to exist as operator-visible
  metadata while metascore still treats those models as normal winners for
  operator-facing light/standard turns. Once a model is admitted only
  narrowly, that policy has to bias selection away from it on ordinary
  orchestrator work unless the request shape actually justifies the niche.
- **Routing RCA must distinguish exclusion from insufficient evidence.**
  The operator surface is not allowed to collapse all “model not chosen”
  outcomes into one opaque candidate list. Routing truth must preserve:
  hard exclusion (for example no tool capability, disabled policy, role
  mismatch), soft demotion (for example `niche`, `under_scrutiny`,
  `hardware_mismatch`), and evidence gaps (for example a model has never been
  exercised for the current intent class). That distinction is what makes
  recommendation-grade guidance possible: “this model was ignored because it
  has not been exercised for TOOL_USE yet” is fundamentally different from
  “this model was blocked because it cannot satisfy tool-bearing requests.”
  That distinction must live in the canonical `routing_chain_built` artifact
  itself, not only in lower-level trace annotations, so operator RCA, persisted
  recommendations, and future automated recovery all consume the same routing
  explanation surface.
- **Capability evidence must use one canonical model identity.**
  Baseline seeding, imported exercise rows, live intent observations, routing
  evidence, and operator recommendations are not allowed to maintain separate
  model-key spaces. If one seam uses `openrouter/openai/gpt-4o-mini`, another
  uses `openai/gpt-4o-mini`, and a third uses bare `gpt-4o-mini`, the system
  can store true evidence while still claiming the model is “unexercised.”
  That is architectural drift, not acceptable uncertainty. Intent-capability
  evidence must be normalized through the same canonical model key that routing
  policy and diagnostics already use. That authority must also resolve bare
  routed names, direct provider-qualified names, and nested execution-provider
  specs as aliases of the same exercised model where appropriate, and routing
  recommendations must only say “not exercised for TOOL_USE” when the
  canonical evidence store truly has no matching intent observations.
- **Intent-scoped exercise must remain matrix-owned, not connector-owned.**
  Exercising only `TOOL_USE`, `MEMORY_RECALL`, or any other intent class is
  allowed only as a filtered projection of the canonical exercise matrix owned
  by the shared exercise orchestrator. CLI flags, HTTP request bodies, and any
  future admin surfaces are not allowed to define their own prompt subsets,
  duplicate intent enumerations, or reinterpret what a capability slice means.
  The exercise factory must validate requested intent classes against the same
  canonical intent taxonomy used by routing evidence and persist the resulting
  rows with the same intent labels. Otherwise baseline evidence becomes
  connector-specific drift instead of one coherent capability truth surface.
- **Further operator-directed model-selection modes are intentionally deferred.**
  Curated or hand-selected routing modes may become worthwhile later, but
  `v1.0.7` intentionally keeps using the existing routing-policy seam plus RCA
  evidence to show where selection behavior actually needs to be hardened
  before introducing more routing modes.
- **Future router consultation must be read-only expert advice, not a second
  routing authority.**
  If the orchestrator later needs help choosing a pinned model during subagent
  composition, it must consult the router through a structured read-side
  recommendation artifact owned by the same routing/evidence policy seam. The
  orchestrator is not allowed to reimplement routing logic in prompt space or
  mutate router truth ad hoc. Any future `recommend-model-for-task-profile`
  style tool belongs to post-`v1.0.7` work and must return recommendation,
  confidence, exclusions, and evidence gaps from the router’s single source of
  truth.
- **Turn identity must remain operator-usable on observability surfaces.**
  A trace row is not allowed to hide the authoritative turn identity behind a
  twelve-character truncation with no copy affordance. Operators must be able
  to inspect and copy the full turn id directly from the observability table,
  and any tooltip or expanded detail view must preserve the same exact id
  without requiring screenshots or database spelunking.
- **Repeated routing-chain builds must preserve causal meaning.**
  The UI is not allowed to treat every repeated `routing_chain_built` event as
  if it were retry churn. A second routing pass after a tool result is normal
  for a tool-bearing ReAct turn; retry, fallback, and post-success guard churn
  are different failure modes. The canonical diagnostics projection must make
  that distinction explicit so operators can tell “follow-up pass after tool
  output” from “same-route retry” or “route widened under failure.”
- **Placeholder assistant scaffolding must be suppressed at the loop boundary.**
  Strings like `[assistant message]` or `[agent message]` are not legitimate
  assistant outputs and must not enter session history, guard comparison, RCA,
  or user-visible results. The loop owner must normalize or drop these
  placeholders before they can trigger repetition churn or contaminate
  diagnostics.
- **Behavior hardening is now an RCA-driven program, not an ad hoc bug queue.**
  Repeated firsthand failures and operator reports with canonical diagnostics
  are the authoritative intake for framework-owned behavior work. Each unique
  failure class must have its own roadmap entry, RCA visibility contract, and
  regression proof. `v1.0.7` is expected to fix the obvious repeated failures
  we can demonstrate, not to claim universal agent perfection.
- **Tool execution truth must be captured at the execution seam.**
  If the loop executed a tool call, the `tool_calls` audit table and the
  canonical RCA summary are not allowed to infer that fact later from partial
  history reconstruction. They must be updated from the same execution-owned
  event so a real tool-bearing turn cannot complete with `tool_call_count = 0`
  or an empty tool audit trail.
- **Personality setup interviewing must be owned by one shared contract.**
  The onboarding interview is not allowed to drift between API route wording,
  CLI/degraded fallback copy, and prompt-level LLM behavior. Pre-interview
  framing, question ordering, and convergence strategy belong to one shared
  interview contract: prime the operator to imagine a concrete archetype
  first, ask for the agent name as the first explicit question, then use
  repeated and differently-phrased behavioral probes to triangulate intended
  operating style instead of treating one shallow answer as sufficient. When
  the operator supplies a reference identity, the interview may use the known
  or inferred traits of that identity only as provisional seed assumptions
  that must be surfaced and confirmed, not as silent canonical truth.
- **Release publication and site distribution are now an explicit ownership
  seam.** A git tag is not operator-facing truth. The GitHub Release object,
  attached assets, `releases/latest`, site sync, and public installer scripts
  together define the live distribution contract. The `v1.0.6` failure showed
  that this seam must be treated as architecture, not release clerical work.
- **Persistent-artifact authorship and authority-layer mutation must stay distinct.**
  A turn that is trying to create or update an enduring operator-visible
  artifact (for example a vault note, document, or workspace file) is not
  allowed to treat semantic-memory or authority-layer mutation as equivalent
  proof of success. Tool selection must privilege artifact-writing operations
  and suppress authority-write tools when the turn does not actually call for
  policy/spec ingestion. Success claims such as “created”, “wrote”, “saved”, or
  “stored” are only valid when the turn has matching artifact-writing evidence,
  not merely inspection output or semantic-ingestion output.

For v1.0.7, the active parity backlog is no longer inferred from the historical
gap sections below. The authoritative remaining scope is:

- [docs/parity-forensics/parity-ledger.md](./parity-forensics/parity-ledger.md)
- [docs/parity-forensics/v1.0.7-roadmap.md](./parity-forensics/v1.0.7-roadmap.md)

The older gap sections remain valuable as closure history and evidence, but
they are not the release-driving backlog anymore.

| Category | Compliant | Gaps |
|----------|-----------|------|
| Connector-Factory Pattern | 8/8 entry points | 0 |
| Pipeline Stage Gating | 16 named stages | 0 |
| Guard Chain | 26 full / 6 stream | 0 |
| Post-Turn Parity (standard/stream) | Enforced by test | 0 |
| Security Claim Composition | Wired (v1.0.2) | **CLOSED** |
| HMAC Trust Boundaries | Active (v1.0.2) | **CLOSED** |
| Context Budget Tiers | L0-L3 config-driven (v1.0.4) | **CLOSED** |
| Memory Injection Guarantee | Two-stage (v1.0.1) | **CLOSED** |
| Topic-Aware Compression | StrategyTopicAware (v1.0.4) | **CLOSED** |
| Feature Parity Across Channels | Documented rationale per preset (v1.0.4) | **CLOSED** |
| Off-Pipeline Surfaces | 3 documented | 0 |
| WebSocket Transport (v1.0.3) | Thin connector | 0 |
| Config Schema Derivation (v1.0.3) | Struct-driven | 0 |
| Pipeline Cache Guards (v1.0.4) | Reject unparsed tool calls | 0 |
| Session-Aware Routing (v1.0.4) | Escalation tracker | 0 |
| Model Lifecycle Policy (v1.0.7) | State + reasoned eligibility filter ahead of metascore | 0 |
| **Agentic Retrieval Architecture (v1.0.5/v1.0.6)** | **Core runtime architecture materially wired** | **cleanup + follow-on gaps remain** |
| **Working Memory Persistence (v1.0.5)** | **Shutdown/startup** | **0** |
| **Post-Turn Reflection (v1.0.5)** | **Episode summaries** | **0** |
| **Release Control Plane (v1.0.7 hardening)** | **Now treated as architecture** | **was drifting in v1.0.6** |
| **Verifier/Critic (v1.0.7)** | **Claim-level verifier with structured contradiction + proof diagnostics** | **0** |

### v1.0.6 Agentic Architecture Layers

| Layer | Component | File | Status |
|-------|-----------|------|--------|
| 2 | Query Decomposer | `decomposer.go` | Wired into RetrieveWithMetrics |
| 5 | Procedural Memory | `retrieval_tiers.go` + migration 040 | Enriched schema + learned_skills |
| 8 | Retrieval Router | `router.go` + `daemon_adapters.go` | Wired into retrieval with production intent signals |
| 11 | Reranker | `reranker.go` | Wired into RetrieveWithMetrics |
| 12 | Context Assembly | `context_assembly.go` | Structured evidence with provenance/authority labels |
| 14 | Verifier/Critic | `verifier.go` + `pipeline_stages.go` | Claim-level verifier with retry, task-hint inputs, action-plan and canonical-source checks, freshness gating, subgoal evidence-support checks, and per-intent proof obligations |
| 16 | Reflection | `reflection.go` | Wired into PostTurnIngest |
| — | Working Memory Persistence | `working_persistence.go` | Wired into Daemon Stop/Start |
| 7 | Graph Facts Persistence | `043_knowledge_facts.sql`, `manager.go`, `retrieval_tiers.go`, `graph.go` | Persisted typed relations with provenance/freshness, reusable traversal API, and retrieved first-class evidence with path/impact traversal |

### Remaining Gaps To Full Vision

| Layer | Component | Status |
|-------|-----------|--------|
| 4 | Parallel Retrieval | Closed in the v1.0.7 worktree; routed tiers now fan out concurrently inside the retriever and merge deterministically in router order |
| 3 | Semantic read-path cleanup | Closed in the v1.0.7 worktree; semantic retrieval now uses an enriched category/key/value FTS corpus and a tier-scoped FTS fallback instead of heuristic SQL |

### v1.0.7 Active Architecture-Led Parity Items

| Roadmap ID | Title | Primary architecture seam |
|------------|-------|---------------------------|
| `PAR-008` | SSE MCP release-claim narrowing | SSE transport confidence must flow through one authoritative named-target validation harness and evidence artifact, with central MCP config conversion plus endpoint-discovery/auth-capable SSE transport semantics. v1.0.7 ships the harness and runtime seam, but the release claim is explicitly narrowed away from proven cross-vendor third-party SSE interoperability. |

---

## Release Control Plane Drift (Discovered After v1.0.6 Tagging)

**Severity**: HIGH
**Architectural principle violated**: operator-facing truth must have one
authoritative control plane

**What happened on 2026-04-19**:

- `v1.0.6` was tagged
- the tag-triggered release workflow failed before publishing a GitHub Release
- `releases/latest` therefore stayed at `v1.0.5`
- the site still served stale installer scripts with different checksum
  expectations than the source repo
- site sync was not triggered from the source release path and also expected
  source-tree registry files that were not present in the tagged tree

**Why this matters**: operators do not install "a tag." They install from the
GitHub Release object, `releases/latest`, `roboticus.ai/install.sh`,
`roboticus.ai/install.ps1`, and `roboticus upgrade all`. If those surfaces do
not agree, the release is structurally incomplete regardless of how clean the
source tag looks.

**Required fix direction**:

1. release workflow must validate the live published release object, not just
   local build artifacts
2. release workflow must fail if the tagged tree does not contain both
   `docs/releases/vX.Y.Z-release-notes.md` and a matching
   `CHANGELOG.md` section for `X.Y.Z`
3. source repo must trigger site sync on publication
4. site sync must copy canonical installer scripts from the tagged source repo
5. site sync must not assume absent release-tree directories are mandatory
6. public site content must not advertise unsupported fallback installs

---

## Gap 1: SecurityClaim Resolvers Defined But Never Called

**Severity**: CLOSED
**Rust principle violated**: Section 5 (Clear Boundaries) — "Authority resolution" belongs in Pipeline

**Current state**: Closed in v1.0.6. Stage 8 (`authority_resolution`) is the live owner for `SecurityClaim` composition. The pipeline resolves channel/API/A2A claims through the proper resolver path, attaches the resolved claim to the session, annotates `authority` and `claim_sources` on the trace, and applies threat-caution downgrade on the live path.

**Rust behavior**: Every entry point constructs a proper SecurityClaim via the corresponding resolver. The claim carries through the entire pipeline and is attached to every tool call for audit.

**Fix**: Completed. Remaining work is transport-by-transport classification and broader cross-layer sandbox audit, not basic claim-owner wiring.

---

## Gap 2: API Routes Never Set Input.Claim

**Severity**: CLOSED
**Rust principle violated**: Section 6 (Feature Parity Across Channels) — all channels access same capabilities

**Current state**: Closed in v1.0.6. API-key routes do not need to synthesize `ChannelClaimContext`; under `AuthorityAPIKey`, Stage 8 resolves the claim through `ResolveAPIClaim(...)`. The old route-level `Input.Claim` scaffolding was removed because it obscured the true live owner.

**Rust behavior**: API requests also go through claim resolution (`resolve_api_claim`), producing a SecurityClaim with source tracking.

**Fix**: Completed by making Stage 8 the canonical API claim owner and removing dead route-layer claim placeholders.

---

## Gap 3: HMAC Trust Boundaries Passive — Model Not Instructed

**Severity**: MEDIUM
**Rust principle violated**: Section 4 (Cognitive Scaffold) — "the framework must preserve the model's reasoning chain"

**Current state**: `internal/agent/hmac_boundary.go` implements HMAC-SHA256 signing and verification. `SanitizeModelOutput()` strips forged markers. But:
- The system prompt (`internal/agent/prompt.go`) never mentions trust boundaries
- The model has no instruction to generate or respect boundaries
- Verification only catches markers that happen to be present (passive defense)

**Rust behavior**: System prompt includes boundary instructions. Boundaries are injected between prompt sections. Model output is verified against expected section structure.

**Fix**: Inject HMAC boundary markers between system prompt sections (personality, firmware, tools). Add verification on model output to detect section tampering. This is the Rust `inject_hmac_boundary` / `verify_hmac_boundary` pattern.

---

## Gap 4: Memory Injection Not Guaranteed — CLOSED (v1.0.1)

**Severity**: HIGH → **RESOLVED**
**Rust principle violated**: Section 4 (Cognitive Scaffold)

**Resolution (v1.0.1)**: Complete overhaul of memory injection architecture:
1. Two-stage injection: `RetrieveDirectOnly()` injects only working + ambient;
   all other tiers accessed via query-aware memory index + `recall_memory`/`search_memories` tools
2. Empty memory index injects orientation marker directing model to `search_memories(query)`
3. Query-aware `BuildMemoryIndex()` surfaces topic-matched entries alongside tier-priority top-N
4. Anti-confabulation behavioral contract prevents model from fabricating memories
5. `search_memories(query)` tool (beyond-parity) gives model on-demand FTS5 + LIKE search

**Files changed**: `daemon.go`, `retrieval.go`, `memory_recall.go`, `prompt.go`, `schema.go`
**Tests**: 15 regression tests in `memory_search_test.go`, `retrieval_direct_test.go`, `client_formats_test.go`
**Remaining**: Skill/subagent execution paths still bypass `buildAgentContext()` (tracked separately)

---

## Gap 5: Context Budget Missing Tier System — CLOSED (v1.0.4)

**Severity**: MEDIUM → **RESOLVED**
**Rust principle violated**: Section 4 (Cognitive Scaffold)

**Resolution (v1.0.4)**: Config-driven context budget tiers:
1. `ContextBudget.L0` through `.L3` fields added to config struct with defaults matching Rust (8K, 8K, 16K, 32K)
2. `SoulMaxContextPct` (0.4 default) caps personality budget
3. `ChannelMinimum` ("L1") enforces minimum tier per channel
4. Hardcoded budget percentages in pipeline/agent replaced with config-driven values
5. `EstimateTokens()` replaces all `len/4` heuristics with UTF-8 aware per-script estimation

**Files changed**: `config.go`, `config_defaults.go`, `tokencount.go` (new), 10+ call sites updated

---

## Gap 6: Topic-Aware History Compression Missing — CLOSED (v1.0.4)

**Severity**: MEDIUM → **RESOLVED**
**Rust principle violated**: Section 4 (Cognitive Scaffold)

**Resolution (v1.0.4)**: `StrategyTopicAware` compression strategy:
1. `CompressWithTopicAwareness()` groups messages by topic using Jaccard keyword similarity
2. Current-topic messages preserved in full; off-topic compressed aggressively
3. New `CompressionStrategy` enum value alongside existing `StrategyTruncate` and `StrategyDropLowRelevance`
4. Uses existing embedding infrastructure for topic similarity detection

**Files changed**: `compression.go`, `topic_compression.go` (new)

---

## Gap 7: Feature Parity — Channel Presets Missing Specialist/Skill — CLOSED (v1.0.4)

**Severity**: LOW → **RESOLVED**
**Rust principle violated**: Section 6 (Feature Parity)

**Resolution (v1.0.4)**: All four preset functions (`PresetAPI`, `PresetStreaming`, `PresetChannel`, `PresetCron`) now carry doc comments with explicit "Stage rationale for non-default values" sections documenting *why* each stage is enabled or disabled per preset:
- `PresetAPI`: SpecialistControls/SkillFirst disabled — API clients manage their own specialist UX
- `PresetStreaming`: GuardSetStream (6 guards) — retry-capable guards excluded from streaming; no nickname mid-stream
- `PresetChannel`: SpecialistControls/SkillFirst enabled — interactive specialist creation + trigger-based skills
- `PresetCron`: DedupTracking/Delegation/Shortcuts disabled — scheduler guarantees uniqueness, tasks self-contained

**Fix**: Add doc comments to each preset function documenting the rationale for any disabled stage, matching the Rust architecture's table format.

---

## Model Lifecycle Policy And Routing Eligibility (v1.0.7)

**Severity**: CLOSED
**Architecture principle extended**: eligibility is decided before ranking.

**Current state**: Closed in v1.0.7. The Go implementation now treats model
policy as an explicit architecture seam instead of an accidental byproduct of
metascore tuning. The policy is no longer only an in-memory/config concern; it
has a persistent operator-managed lifecycle store with merge semantics against
configured defaults.

- per-model lifecycle state is configurable and inspectable:
  - `enabled`
  - `niche`
  - `disabled`
  - `benchmark_only`
- per-model role eligibility is configurable and inspectable:
  - orchestrator
  - subagent
- every lifecycle decision may carry:
  - primary reason code
  - secondary reason codes
  - operator-readable reason text
  - evidence references
  - source
- policy is resolved centrally from:
  - configured defaults
  - persisted operator overrides
  - canonical normalization of provider-qualified model specs
- live routing now filters candidates by lifecycle state and role eligibility
  before metascore or heuristic ranking runs
- benchmark/exercise selection uses the same lifecycle policy seam instead of
  a second ad hoc allowlist

**Architectural rule**: metascore is not allowed to stand in for hard policy.
A model that is disabled or benchmark-only must never enter the live routing
pool. A model that is subagent-only must never be considered for
operator-facing orchestration. Ranking happens only inside the surviving
eligible set.

**Why this matters**: the benchmark program has already shown that “installed”
and “live-routable on this hardware” are not the same thing. Without explicit
model policy states, the runtime keeps overloading ranking heuristics to solve
a lifecycle-management problem they were never meant to own.

**Architectural rule**: policy resolution happens once, centrally, and is then
reused by live routing, benchmark selection, diagnostics, and operator/admin
surfaces. State transitions must remain reasoned and evidenced; hidden
blocklists are not an acceptable substitute.

**Documentation follow-through**: the primary C4 document
(`docs/diagrams.md`) now reflects this seam directly. Model lifecycle policy,
benchmark history, canonical turn diagnostics, and the operator-facing
WebSocket/webchannel observability surface are shown as first-class containers
instead of being implied only by code or supplementary rules diagrams.

---

## WebSocket-First Dashboard Architecture (v1.0.3)

**Severity**: N/A (new capability, not a gap)
**Architectural assessment**: COMPLIANT

The v1.0.3 WebSocket-first dashboard replaces all HTTP polling with topic-based subscriptions. Key architectural properties:

1. **Thin connector**: `ws_protocol.go` handles upgrade, ticket validation, and message framing only. No business logic.
2. **Pipeline bridge**: Pipeline stages publish lifecycle events (session start/end, trace, health) to the EventBus. The WS layer subscribes and broadcasts — it does not query or transform.
3. **Ticket authentication**: WS connections require a pre-validated ticket (anti-CSRF, anti-replay). Ticket issuance is in the API route layer; validation is in the WS upgrade handler.
4. **Topic isolation**: `ws_topics.go` defines a registry of subscribable topics. Clients subscribe to specific topics; the server does not broadcast everything to everyone.
5. **Zero polling**: All `setInterval`-based polling removed from dashboard. All state updates arrive via WS push.

This is a transport-layer change. The pipeline remains the single behavioral authority. The WS layer is a delivery connector, analogous to the existing SSE streaming connector.

---

## v1.0.4 Architectural Changes

### Pipeline Stage Extraction
`pipeline.Run()` refactored from 874-line monolith to 16 named stage methods operating on a `pipelineContext` struct. Each stage returns `(*Outcome, error)` or mutates context. Zero behavioral change — all existing tests pass unchanged. This is the first step toward pluggable stage pipelines.

### Security Hardening
- `Store.DB()` deleted — no raw `*sql.DB` access. Architecture test prevents re-introduction.
- Wallet passphrase keystore-only — env var fallback and machine-derived passphrase removed.
- Cache guards reject responses containing unparsed tool call JSON (`"tool_call"`, `"function_call"`).
- All credential config fields (`APIKeyEnv`, `TokenEnv`, `PasswordEnv`) removed — keystore is the only credential store.

### Session-Aware Model Routing
`SessionEscalationTracker` monitors per-session inference quality. On 2+ consecutive failures or quality < 0.3 for 3+ turns, the router escalates to a higher-capability model. This is stateful routing — the router maintains session context, not just per-request signals.

### Financial Action Verification
`FinancialActionTruthGuard` added to the guard chain (26th guard). Before a pipeline response claiming financial success is delivered, the guard verifies the claimed action against tool execution output. Prevents fabricated trading/transfer results.

### Cross-Layer Security / Sandbox Truth Ownership
v1.0.6 also tightened a cross-cutting architectural seam that had been too
implicit in earlier releases:

- Stage 8 is the live owner for `SecurityClaim` composition.
- Policy evaluation and tool/runtime path resolution now share substantially
  tighter sandbox and config-protection semantics.
- Post-inference truth guards have been narrowed so they preserve real
  policy/sandbox denials and failed execution outcomes instead of flattening
  every denial-shaped answer into a fake-capability case.

This is not just "more guards." It is an ownership correction: policy and tool
runtime define what actually happened; post-inference guards are only allowed to
police fabricated narration around that outcome.

### v1.0.6 Final Closure Verdict

The parity program for v1.0.6 is now **decision-complete**.

- Every scoped parity system has a final disposition in
  `docs/parity-forensics/parity-ledger.md`.
- The codebase was materially strengthened in ways that are now backed by both
  runtime tests and durable architecture documentation.
- Prompt compression is **not** part of that strengthening story for this
  release. It failed the corrected history-bearing soak gate and now has an
  explicit benchmark-only disposition: disabled by default, not recommended for
  live use, and retained only for controlled comparison work.

The explicit release-readiness answer for v1.0.6 is:

**Yes** — the code was materially strengthened, and the docs now record that
strengthening truthfully, with the remaining deferred items called out
explicitly rather than hidden behind vague unresolved language.

### Final Audits

#### Architectural Audit

- Single ownership is now explicit for the highest-risk seams:
  request construction, tool pruning, routing truth, checkpoint lifecycle,
  plugin runtime lifecycle, webhook normalization, MCP runtime tool sync, and
  policy/sandbox truth.
- The major shadow-path contradictions found during parity work were either
  removed or demoted out of the live path.
- Durable docs now reflect the validated ownership model rather than the older
  generic container story alone.

#### Functional Audit

- Release-facing claims are supportable by the runtime and tests.
- Channel, scheduler, MCP, cache, and guard behavior now match their documented
  operator contracts closely enough to treat remaining differences as accepted
  deviations or explicit deferrals.
- Prompt compression is clearly benchmark-only and is not being presented as a
  release-ready feature or live optimization.

#### Fitness Audit

- Test coverage now pins the newly closed seams directly, including request
  artifact invariants, selected tool-surface reuse, routing trace truth,
  checkpoint lifecycle, MCP timeout/tool-surface truth, and route-family
  observability contracts.
- Observability surfaces are materially more truthful: canonical route-family
  ownership is explicit, dead MCP transports no longer masquerade as healthy,
  and release notes now function as audited truth surfaces rather than vague
  confidence prose.
- The recent fixes reduced ambiguity and drift overall; they did not add new
  broad subsystems or placeholder abstractions.

### v1.0.7 Root Cause Analysis + Final Parity Goal

v1.0.7 should be treated as the **Root Cause Analysis build** for inference
stalling and fallback behavior, and as the release that takes the remaining
post-v1.0.6 deferred parity edges to final disposition.

The current runtime can show that inference ran long and that a provider
eventually timed out, but it still cannot attribute the delay precisely enough
to distinguish:

- bad route choice
- provider queueing or cold start
- machine saturation
- time-to-first-header failure
- fallback-chain delay

That is a post-v1.0.6 architecture goal, not a v1.0.6 release blocker. The
next release should add first-class per-attempt timing, fallback-chain
attribution, router health inputs, and user-visible stall/reroute status so the
system can explain and react to this class of failure from runtime truth rather
than operator guesswork. It should also take the remaining accepted/deferred
parity edges from v1.0.6 and either retire them, redesign them, or close them
with explicit final rationale rather than leaving them as indefinite release
residue.

### Request Artifact Ownership
v1.0.6 clarified that the final `llm.Request` is itself an architectural
artifact, not just a local implementation detail. The validated ownership is:

- Stage 8 / 8.5 prepare authority and memory artifacts.
- Tool pruning writes the selected structured tool surface before request
  assembly.
- `ContextBuilder.BuildRequest` owns final message assembly, including
  checkpoint digest restore, history compaction/compression, and prompt-layer
  tool roster alignment.
- Routing trace and model-selection audit must reflect that actual request,
  rather than a synthetic user-only approximation.

This closes an important class of migration errors where parity-looking helper
code existed, but the actual inference artifact was still assembled by weaker
or duplicate paths.

### Continuity / Learning Artifact Ownership
v1.0.6 also moved continuity work closer to long-term architecture instead of
release-specific patching:

- checkpoint save/load/prune now share repository-owned lifecycle seams
- reflection reads real turn artifacts instead of zero-duration and adjacency
  proxies
- `episodic_memory` now stores both a compact human-readable summary and a
  structured `content_json` payload
- consolidation prefers the structured payload over reparsing compact text

That is an architectural shift toward durable, machine-consumable turn state.
It materially lowers the risk of future drift caused by helper-specific string
formats becoming accidental downstream contracts.

---

## Compliant Areas (No Gaps)

### Connector-Factory Pattern ✓
All 8 entry points use `pipeline.RunPipeline()`. No business logic in connectors. Architecture tests enforce:
- `TestArchitecture_RoutesDontImportAgent`
- `TestArchitecture_ConnectorFilesInvokeRunPipeline`
- `TestArchitecture_ConnectorsDoNotContainPolicyDecisions`
- `TestArchitecture_ConnectorFilesAreStructurallyThin` (line limits)

### Pipeline Stage Gating ✓
All 13 boolean flags and 4 enums are checked in `Run()`. No unconditional stages.

### Guard Chain ✓
26 guards in Full chain (was 25 — added `FinancialActionTruthGuard` in v1.0.4), 6 in Streaming chain. Cached uses Full. All registered in `DefaultGuardChain()`.

### Post-Turn Parity ✓
Standard and streaming paths both run memory ingest, embedding, observer dispatch, and nickname refinement through the pipeline. Enforced by `TestMandate_StreamingCallsFinalizeStream`.

### Injection Defense ✓
4 layers deployed. L1/L2 in pipeline stage 2 for all entry points. L4 in agent loop after every tool execution. Unicode normalization, homoglyph folding, zero-width stripping.

### Tool Execution ✓
Policy denials soft-fail with structured reason. Error dedup suppresses repeated failures. L4 output scan on every tool result. Sequential execution with loop detection.

### Off-Pipeline Surfaces ✓
3 documented exemptions (interview, session analysis, turn analysis). All use `llmSvc.Complete()` directly for analytics, not agent inference.

---

## Prioritized Fix Order

| Priority | Gap | Effort | Impact |
|----------|-----|--------|--------|
| ~~P0~~ | ~~Gap 4: Memory injection not guaranteed~~ | **CLOSED v1.0.1** | Two-stage injection + search_memories tool |
| ~~P1~~ | ~~Gap 1: SecurityClaim resolvers not wired~~ | **CLOSED v1.0.2** | Resolvers called at stage 8, claim stored on session + trace |
| ~~P1~~ | ~~Gap 5: Context budget missing tier system~~ | **CLOSED v1.0.4** | L0-L3 config-driven, EstimateTokens(), SoulMaxContextPct |
| ~~P2~~ | ~~Gap 3: HMAC boundaries passive~~ | **CLOSED v1.0.2** | Prompt now includes boundary instructions |
| ~~P2~~ | ~~Gap 6: Topic-aware compression missing~~ | **CLOSED v1.0.4** | StrategyTopicAware with Jaccard similarity grouping |
| ~~P3~~ | ~~Gap 2: API routes never set Claim~~ | **CLOSED v1.0.2** | Both API routes now construct ChannelClaimContext |
| ~~P3~~ | ~~Gap 7: Preset doc comments missing~~ | **CLOSED v1.0.4** | All 4 presets carry stage rationale doc comments |

**All 7 original gaps are CLOSED.** That does **not** mean the parity or
architecture program is complete. Open architectural/parity work still remains
in request shaping, MCP transport semantics, cache/replay semantics, and the
cross-cutting scheduler/plugin/channel families tracked in
`docs/parity-forensics/`.
