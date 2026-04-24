# Verifier Sensitivity Diagnosis

This document captures the current diagnosis for the self-negating completion
plague without proposing code changes yet. The working claim is that the plague
is not one bug. It is at least three:

1. forbidden canned terminal output exists at all
2. verifier/retry/finalization reaches that terminal path far too often
3. benchmark scoring then misreads framework-shaped degraded output as model
   success

The main focus here is bug 2: verifier sensitivity.

## Updated State After Phase 1

The ternary memory-confidence correction and canned-response purge changed the
residual failure shape materially.

Fresh rerun:

- run id: `44443e5f17f25ed6923fe488a6befdc3`
- model: `moonshot/kimi-k2.6`
- result:
  - pass/fail: `16 / 19`
  - avg quality: `0.83`

Important directional changes from the baseline:

- the self-negating verifier boilerplate disappeared from the rerun output
- average quality improved
- coding quality improved sharply
- the dominant remaining visible failure mode is now `empty response`

That means phase 1 did what it was supposed to do: it reduced one major false
failure family and exposed the next architecture seam more clearly.

## Current Working Thesis

The strongest current explanation is:

1. broad retrieval policy routes many question-shaped prompts into retrieval
2. broad memory-gap detection treats missing memory tiers as explicit gaps
3. verifier uses those gaps to trigger `unsupported_certainty`
4. retry and recheck do not clear the issue often enough
5. finalization collapses the turn into forbidden canned output

This means the system is often treating "we do not already remember this" as if
it meant "the answer lacks required support." That is a category error.

## Empirical Evidence

### Benchmark Evidence

From `/Users/jmachen/.roboticus/state.db`:

- `exercise_results` rows total: `974`
- rows containing the exact canned phrase
  `"I can't honestly claim success because the final verification still failed"`:
  `16`
- all `16` of those rows were marked `passed`

Intent distribution of the canned rows:

- `CODING`: `6`
- `INTROSPECTION`: `4`
- `EXECUTION`: `3`
- `TOOL_USE`: `2`
- `CONVERSATION`: `1`

Those rows cluster in categories where degraded truthful output should still be
useful to the operator.

The dominant visible reason family in those rows is explicit-gap / certainty
failure:

- rows mentioning explicit gaps: `12/16`
- rows mentioning partial coverage: `4/16`
- rows mentioning retrieved-evidence mismatch: `2/16`
- rows mentioning proof/high-risk style issues: `2/16`

Representative prompts among the canned rows include:

- `What time is it?`
- `What is 2 + 2?`
- `In Go, what does len(slice) return when the slice is nil?`
- `Write a function in any language that reverses a string in-place and explain one edge case to watch for.`

Those are damning examples because they are derivable or ordinary direct-knowledge
prompts. They should not need prior memory to avoid a verifier penalty.

### Live RCA Evidence

Over the last 14 days in `turn_diagnostic_events`:

- `verifier_retry_scheduled`: `214`
- `verifier_retry_rechecked`: `66`
- `verifier_retry_suppressed`: `42`
- `verifier_retry_failed`: `7`

Primary issue-code distribution for `verifier_retry_scheduled`:

- `unsupported_certainty`: `61`
- `subgoal_coverage`: `23`
- `artifact_set_overclaim`: `5`
- `artifact_content_mismatch`: `3`
- `ignored_contradictions`: `2`
- `unsupported_absolute_claim`: `1`
- `proof_obligation_unmet`: `1`

Primary issue-code distribution for `verifier_retry_rechecked`:

- `unsupported_certainty`: `42`
- `artifact_set_overclaim`: `4`
- `subgoal_coverage`: `2`
- `artifact_content_mismatch`: `2`
- `unsupported_subgoal`: `1`

Primary issue-code distribution for `verifier_retry_failed`:

- `unsupported_certainty`: `3`
- `subgoal_coverage`: `2`
- `unsupported_absolute_claim`: `1`
- `proof_obligation_unmet`: `1`

That makes the current diagnosis hard to avoid: verifier sensitivity is being
driven mainly by `unsupported_certainty`, not mainly by contradiction or severe
proof failure.

## Architectural Root Of The Sensitivity

The sensitivity is not appearing from nowhere.

### 1. Retrieval is broad

Question-shaped turns are routed into retrieval by default. That is already a
wide policy surface.

### 2. Memory-gap detection is too coarse

`detectGaps()` in
[`internal/agent/memory/context_assembly.go`](/Users/jmachen/code/roboticus/internal/agent/memory/context_assembly.go)
marks a gap when memory tiers such as episodic, semantic, procedural, or
relationship have no hits.

That is not the same as "required task evidence is missing."

### 3. Coarse gaps become verifier certainty failures

`HasGaps` flows into the verifier, and the verifier uses it to trigger
`unsupported_certainty` when the answer sounds fully certain.

This collapses two very different ideas:

- no prior memory hit
- no adequate support for the answer

That collapse is the current leading cause.

## Trinary Memory-Confidence Model

The more coherent rule is:

- `-1`: memory contradicts the answer
- `0`: memory is absent, irrelevant, or the answer is derivable in-turn and
  therefore mutable rather than something expected to pre-exist in memory
- `+1`: memory reinforces the answer

Under that model, memory is a confidence modifier, not a universal proof gate.

This is especially important for:

- arithmetic
- current-time queries
- direct code reasoning
- results derived from current files, runtime state, or local analysis

For those classes, absent memory should be neutral, not negative.

## Counterfactual Simulation

Applying the trinary memory-confidence rule to the currently observed canned
benchmark rows:

### Likely neutral-memory rows

These rows should not have been penalized for missing memory:

- `What time is it?`
- `What is 2 + 2?`
- nil-slice and pointer-safety coding prompts
- simple string-reversal coding prompt

Estimated count: roughly `10/16` current canned benchmark rows.

Counterfactual outcome:

- the explicit-gap / unsupported-certainty failure would likely disappear
- these rows would more likely remain ordinary pass or degraded-pass rows
  instead of terminalized canned failures

### Ambiguous decomposition rows

These rows look more like subgoal/decomposition problems than memory problems:

- config summarization
- introspection prompts with two-part asks
- recent-performance comparison prompts

Estimated count: roughly `4/16`.

Counterfactual outcome:

- memory would stop making them worse
- they may still degrade on `subgoal_coverage` or `unsupported_subgoal`

### Possibly legitimate verifier-sensitive rows

These rows may still deserve stronger proof handling:

- higher-risk architectural/security design prompts
- broader self-evaluation prompts with large historical claims

Estimated count: roughly `2/16`.

Counterfactual outcome:

- these may still degrade even after the memory-confidence fix because the
  issue is more plausibly proof burden than memory absence

## Strongest Current Conclusion

Most observed canned benchmark rows do not look like honest model failures.

They look like framework-shaped verifier failures caused by:

- over-broad retrieval
- over-broad gap detection
- certainty checks tied to coarse `HasGaps` semantics
- binary finalization policy

The benchmark then compounds the problem by scoring the resulting response as a
pass.

## New Residual Failure Analysis

The fresh rerun shows the next dominant failure family is not model silence.

Using the rerun rows aligned to canonical `turn_diagnostics`, the residual
`passed = 0` set breaks down as:

- `provider_reasoning_content_mismatch`: `18`
- `max_turns_exceeded`: `1`
- evidence of raw model-empty finalization: `0`

The dominant event pattern is:

1. first model attempt succeeded
2. tool call finished successfully
3. the system made another model call
4. that later call failed with:
   - `thinking is enabled but reasoning_content is missing in assistant tool call message`
5. the turn degraded to empty and benchmark recorded `empty response`

This is not a “the model returned nothing” story. It is a framework-shaped
post-observation failure.

### What this changes

The main residual seam is no longer accurately described as “second attempt”
logic. That framing is wrong because it implies we are simply retrying the same
kind of work.

The real problem is:

- after authoritative observation, the system is supposed to enter a different
  reasoning mode
- that mode should interpret, validate, and refine
- it should not silently collapse back into ordinary open-ended execution
  inference

So the architectural bug is:

**post-observation reasoning is under-specified and is currently being routed
through generic inference semantics instead of a narrower reflect contract.**

The official execution model is `R-TEOR-R`:

- leading `R`: retrieval memory before the turn acts
- `T`: think / plan
- `E`: execute
- `O`: observe
- `R`: reflect / self-assess
- trailing `R`: retention memory after the turn completes

That split matters because the two `R` phases are not the same operation:

- pre-loop retrieval memory contributes confidence, contradiction checks, and
  prior evidence
- post-loop retention memory decides reinforcement, decay, and what deserves to
  persist as future evidence

### Reflect Contract

After successful tool-backed observation, the next phase should do only four
things:

1. interpret what happened
2. validate whether the observed result satisfies the task
3. refine how the result is presented to the operator
4. decide explicitly whether more execution is actually required

It should not reopen execution by default.

### TOTOF Reflection Artifact

The reflection seam should not consume a universal raw transcript. It should
consume one canonical reflective artifact, rendered safely per provider/model
capability combination.

That canonical artifact is `TOTOF`:

- `T`: the user task
- `O`: the authoritative observed results
- `T`: the key tool outcomes
- `O`: any unresolved gaps or contradictions
- `F`: a bounded instruction to interpret and finalize

The invariant is `TOTOF`, not the literal wire transcript. Reflection renderers
may differ by provider, model family, tool-call semantics, and thinking-mode
support, but they should all be rendering the same canonical `TOTOF` state.

Reflection continuation also needs one explicit rule: if more execution is
required after observation, that decision must be explicit. The signal may be
textual (`CONTINUE_EXECUTION ...`) or structural (tool calls returned from the
reflect request). In either case, the framework should treat it as a request
to continue execution rather than flattening it into final operator prose.
But that continuation is not allowed to append a prose system note and reopen
generic session replay. It must be rendered from a canonical continuation
artifact derived from the latest `TOTOF` state plus the explicit remaining-work
reason, so provider/model adapters can continue safely without inheriting raw
assistant/tool-call history from the previous execution cycle.

### Updated Strongest Conclusion

The residual `empty response` failures are now mostly explained by:

- successful execution
- followed by a wrongly-shaped post-observation reasoning phase
- which re-enters provider-facing generic inference and can destroy the already
  successful turn

That means the next correction target is not just verifier tuning. It is the
architectural boundary between:

- pre-execution `think`
- post-observation `reflect`

## Planned Correction Sequence

The corrective path should be phased rather than improvised:

### Phase 1. Apply ternary memory confidence and retest

First correction:

- treat memory as `-1 / 0 / +1` influence on confidence
- stop treating absent or irrelevant memory as evidence failure
- rerun benchmark and soak slices after the change
- measure how much of the current plague disappears

The output of this phase is evidence, not victory theater.

### Phase 2. Investigate the remaining contra-indicated results

After the ternary memory correction, inspect the rows that still degrade or
trigger verifier retry.

This phase now has a sharper success criterion:

- prove whether any meaningful residuals are still memory-shaped
- if not, pivot cleanly to reflect/verifier/post-observation policy

Residual RCA from the fresh rerun strongly suggests that memory is no longer
the dominant cause. The next gap is the reflect seam.

Questions for this phase:

- what percentage is still driven by verifier tuning?
- what percentage is mostly `subgoal_coverage` / `unsupported_subgoal`?
- what percentage is genuine contradiction, proof burden, or model weakness?

The goal is to avoid smearing every remaining bad row into one bucket.

### Phase 3. Correct based on the empirical split from phases 1 and 2

Only after phases 1 and 2 should further correction be applied.

That correction should target the most probable remaining causes, such as:

- verifier sensitivity / certainty thresholds
- subgoal decomposition or coverage policy
- proof-obligation scoping
- benchmark grading or cohort defects

This keeps the fix sequence empirical instead of kneejerk.

## Investigation Questions Still Open

These questions remain worth answering before code changes:

1. Exactly how often are derivable prompts entering retrieval and then picking
   up `HasGaps`?
2. How often does `subgoal_coverage` act as the main blocker after
   `unsupported_certainty` is removed from the picture?
3. Which live-turn categories, not just benchmark rows, are most affected by
   verifier sensitivity?
4. Which persisted derived signals are contaminated enough to need quarantine,
   recalculation, or selective purge after the fix?

## Persistence Hygiene Implication

If verifier sensitivity is manufacturing degraded benchmark outcomes and those
rows are being persisted as successful evidence, some stored learning signal is
likely contaminated.

The disciplined response is not a wipe. It is:

1. preserve forensic evidence
2. identify contaminated derived signals
3. quarantine or exclude them from routing / benchmark / learning inputs
4. selectively purge or recalculate only what is proven contaminated

Full wipe should remain off the table unless selective isolation is provably
impossible.
