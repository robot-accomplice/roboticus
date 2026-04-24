# Benchmark RCA Checklist

Use this checklist before any benchmark, exercise scorecard, routing
recommendation, or release note claims model efficacy.

This document exists because benchmark output can be structurally dishonest:
the run may mix true model weakness with harness defects, server unavailability,
timeout policy mistakes, rescue-path distortion, or grader drift. When that
happens, the correct action is RCA, not ranking.

## Purpose

- Separate **model failure** from **benchmark-system failure**.
- Force ambiguous rows into an **invalid** class instead of silently poisoning
  rankings.
- Preserve enough artifact detail that operators can explain why a score is
  weak, rescued, degraded, or unusable.

## Trigger Conditions

Run this checklist when any of the following appear in benchmark or exercise
output:

- `empty response`
- `context deadline exceeded`
- connection failures or server-unreachable errors
- repeated category-specific failure concentration
- suspiciously hedged `PASS` outputs ("I can't honestly claim success..."-type
  language)
- repeated verifier-shaped self-negating completions where the model produces a
  mostly useful answer but wraps it in final-failure language
- high latency with mixed pass/fail behavior
- obvious disagreement between category-level quality and final top-line score

## Required Top-Level Statuses

Benchmark rows are not allowed to collapse to binary `PASS` / `FAIL`.
Every row must be classified as one of:

- `clean_pass`
- `rescued_pass`
- `degraded_pass`
- `model_fail`
- `invalid`

Use `invalid` when the row cannot honestly be attributed to model capability.

## RCA Taxonomy

Every failed, degraded, rescued, or invalid row must map to one primary RCA
bucket and may carry secondary buckets.

### 1. Infrastructure Failure

Use when the benchmark target was not available or the run environment was not
credible.

Examples:

- server unavailable
- provider/model unreachable
- host saturation
- request timeout waiting for headers
- benchmark runner concurrency/load artifact

Typical classification:

- almost always `invalid`

### 2. Harness / Protocol Failure

Use when the benchmark harness failed to obtain, parse, persist, or classify a
 result truthfully.

Examples:

- `empty response` with no preserved provider envelope
- malformed response parsing
- silent truncation
- retry behavior not surfaced in artifacts
- missing runtime-state snapshot
- missing host-resource snapshot

Typical classification:

- usually `invalid`
- occasionally `degraded_pass` if the final answer is still clearly attributable

### 3. Grading / Evaluation Failure

Use when the output exists, but the benchmark rubric is flattering or
misclassifying behavior.

Examples:

- polished wrong answer receives a pass-like score
- rescued output counted as a clean pass
- memory/tool misuse hidden by answer-only grading
- over-synthetic prompt that rewards benchmark-specific pattern matching
- self-negating completions counted as ordinary model prose instead of a
  verifier/finalization defect signal
- concise contract-satisfying answers score poorly because the rubric rewards
  verbosity, generic structure, or irrelevant intent markers

Typical classification:

- row status may vary, but the cohort is under defect until fixed

### 4. Model Capability Failure

Use when the runtime was healthy, artifacts are complete, and the model still
failed the task.

Examples:

- wrong answer with adequate evidence available
- failure to call required memory/tool path when model had the opportunity
- confabulated memory answer when retrieval evidence is absent
- repeated inability to complete the category under healthy conditions

Typical classification:

- `model_fail`
- or `rescued_pass` / `degraded_pass` if the system recovered the turn

### 5. Benchmark Design Failure

Use when the benchmark case or cohort itself is not a reliable measurement
instrument.

Examples:

- prompt too synthetic or overfit to the harness
- category imbalance hides serious weakness
- stale or invalid case kept in the matrix
- rescue policy masks real weakness
- task depends on a capability the harness does not actually expose correctly

Typical classification:

- cohort validity defect
- affected rows must not drive routing or release claims

## Row-Level Triage Checklist

For each suspicious row, answer these questions in order:

1. Was the server/provider/model reachable for the full request?
2. Were host-resource and model-runtime snapshots captured?
3. Is the raw provider response or canonical failure envelope preserved?
4. Did the model return no content, malformed content, or simply wrong content?
5. Did any rescue, retry, verifier, or fallback path fire?
6. If rescue fired, was the row labeled `rescued_pass` or `degraded_pass`
   rather than `clean_pass`?
7. Did the grader inspect tool use, retrieval grounding, and contradictions, or
   only the final prose?
8. Did the grader evaluate the prompt's actual answer contract, or just generic
   intent heuristics?
9. Was the response penalized for being concise when the prompt actually wanted
   a concise direct answer?
10. If historical artifacts are complete, should this row be rescored under the
    current rubric before any rerun is demanded?
11. Is the prompt/case representative of the category it claims to measure?
12. Does the category show concentrated failures that exceed the rest of the
   matrix?
13. Should this row count toward model ranking, benchmark RCA only, or neither?
14. Does the displayed aggregate use the same denominator semantics as the
    persisted scorecard, or are summary and storage silently disagreeing?
15. Do phase timings show that the row was model-latency-bound, tool-bound,
    retry-bound, or framework-bound?

If questions 1-3 cannot be answered from persisted artifacts, the row is
`invalid` by default.

## Cohort-Level Triage Checklist

After row-level triage, evaluate the cohort:

- Are invalid rows clustered around one category?
- Are failures concentrated in memory recall, tool use, delegation, or
  execution while conversation remains clean?
- Are long-latency passes disproportionately rescued or hedged?
- Are rescue paths inflating top-line success?
- Are prompt cases outdated, duplicated, or under-specified?
- Does the cohort still represent the live failure modes we care about?

If a cohort has unresolved design defects, it must be marked:

- `RCA-hold`

and it must not drive routing recommendations or release claims.

## Prompt-Contract Scoring Rules

Exercise scoring must be prompt-aware without degenerating into exact-string
 matching.

- score the task contract before generic style markers
- treat concision as positive when the prompt asks for a direct bounded answer
- treat tool use as required only when the prompt actually needs tools
- treat tool use as neutral or negative when the prompt is directly derivable
- treat formatting requirements as prompt-specific instead of globally inferred
- preserve the ability to rescore historical rows when prompt and raw response
  artifacts are present
- when the prompt asks for runnable code, prefer artifact execution truth:
  parse/typecheck/compile and bounded input/output correctness before prose
  style heuristics

## Sample Failure Pattern This Checklist Is Designed To Catch

The following mix is a benchmark-RCA incident, not a trustworthy model ranking:

- repeated `empty response`
- `context deadline exceeded`
- category-skewed failure concentration (`MEMORY_RECALL`, `EXECUTION`,
  `DELEGATION`, `TOOL_USE`)
- clean-looking `CONVERSATION` passes alongside unstable operational categories
- pass rows with hedged non-success language
- repeated "I can't honestly claim success..." outputs even when the body
  contains substantial useful work

Interpretation:

- the run is mixing infrastructure instability, possible harness defects, and
  genuine category weakness
- the run may also be mixing framework-shaped verifier/finalization behavior
  with true model behavior
- average quality from such a run is not fit for routing decisions

## Special Epidemic Pattern: Self-Negating Completion

Treat repeated "I can't honestly claim success because the final verification
still failed..." outputs as a top RCA concern, not a style quirk.

Why this matters:

- it pollutes benchmark grading by turning useful-but-imperfect work into
  theatrically failed prose
- it can hide whether the underlying issue is proof coverage, verifier
  overreach, retry/finalization policy, or genuine model inability
- once it becomes common, average quality and pass/fail counts both become less
  trustworthy

Checklist for this pattern:

1. Did the model produce a materially useful answer before the final
   self-negating sentence?
2. Did verifier retry/finalization collapse a revisable answer into a canned
   failure close?
3. Were the remaining issues execution-critical, or merely evidence/coverage
   gaps that should have yielded a degraded answer instead of pseudo-refusal?
4. Is the benchmark grading this as:
   - clean pass
   - rescued/degraded pass
   - model fail
   - or framework-induced invalid row
5. Is the phrase concentrated in specific categories (`EXECUTION`, `CODING`,
   `TOOL_USE`) that suggest verifier/policy mismatch rather than general model
   weakness?

If the phrase appears repeatedly across healthy runtime conditions, open a
framework-owned defect and track it separately from raw model capability.

## Release-Gate Rules

Before a benchmark can support `v1.0.7` claims:

- no benchmark report may present only `PASS` / `FAIL`
- `invalid` rows must be reported separately and excluded from model ranking
- rescued/degraded rows must be visible in scorecards
- benchmark RCA must distinguish:
  - model failure
  - infrastructure failure
  - harness/protocol failure
  - grading failure
  - benchmark design failure
- category-level failure concentration must be surfaced
- cohorts with unresolved RCA defects must be blocked from routing or release
  claims

## Scope Truth Rules

Benchmark reporting must preserve the scope the operator actually requested.

- if the operator exercises an explicit subset of models, the run itself must
  execute only that subset
- the detailed per-model results for the run must stay centered on the freshly
  exercised subset
- the comparison table may and should include all historically exercised models
  so the fresh run is shown in landscape context
- freshly exercised models must be highlighted explicitly, and the table must
  not silently collapse to a fake one-row leaderboard when historical scorecard
  data exists

## Special Epidemic Pattern: Verifier Sensitivity

Treat repeated verifier retries driven by `unsupported_certainty`,
`subgoal_coverage`, or `unsupported_subgoal` as a separate framework RCA path,
not just "strict quality control."

Why this matters:

- verifier sensitivity can punish derivable, ordinary, or in-turn-computable
  answers for lacking irrelevant retrieval support
- over-broad memory-gap semantics can manufacture evidence failure where there
  is only absent prior memory
- once verifier sensitivity is high enough, the benchmark is no longer mostly
  measuring the model; it is measuring framework overconstraint

Checklist for this pattern:

1. Did the turn route through retrieval simply because it was question-shaped,
   even though the answer was derivable in-turn?
2. Did missing memory tiers get treated as a verifier-relevant gap even though
   no task-critical evidence was actually absent?
3. Was `unsupported_certainty` triggered by broad `HasGaps` semantics instead
   of a real contradiction or proof failure?
4. Did `subgoal_coverage` / `unsupported_subgoal` fire on a decomposition that
   was stricter than the benchmark case warranted?
5. Is the final row better explained as framework sensitivity than as model
   inability?

If the answer to questions 2-3 is yes, open a verifier-sensitivity defect and
exclude the row from clean model-efficacy claims until the policy is corrected.
- a single-model targeted exercise must not render leaderboard language such as
  `#1`, `best per intent`, or "ranked by quality" as if a real comparison took
  place
- benchmark output is not allowed to imply cross-model superiority when only
  one model was exercised in the current run

## Required Artifacts

Each benchmark run must preserve enough evidence for this checklist:

- prompt/case identity
- intent/category label
- top-level row status (`clean_pass`, `rescued_pass`, `degraded_pass`,
  `model_fail`, `invalid`)
- correction / rescue events
- raw or canonical provider failure envelope
- host-resource snapshot
- provider/model runtime-state snapshot
- latency
- grader rationale
- final RCA bucket assignment

## Output Expectations

Every benchmark review should produce:

- row-level RCA classifications for suspicious rows
- a benchmark-case defect log
- a cohort validity summary
- a list of rows or cohorts excluded from ranking
- concrete actions:
  - fix harness
  - fix grader
  - relabel/remove cases
  - investigate model weakness
  - rerun under healthy conditions
