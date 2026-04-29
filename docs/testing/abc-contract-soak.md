# ABC Contract Soak Plan

## Purpose

This soak exists to prove whether the ABC-derived guard/RCA changes improve
agent reliability and diagnostic truth. It is not a broad model benchmark and
it is not a marketing claim. The goal is a before/after picture that separates:

- user-visible behavior quality
- framework recovery behavior
- guard/verifier contract evidence quality
- memory-confidence behavior
- model/provider latency and transport validity

Source credit: ABC refers to "Agent Behavioral Contracts: Formal Specification
and Runtime Enforcement for Reliable Autonomous AI Agents"
([arXiv:2602.22302](https://arxiv.org/pdf/2602.22302)).

## Baseline Discipline

The baseline must be captured before additional ABC aggregation changes land.
The baseline commit is the current pre-aggregation v1.0.8 state:

```text
b20bb69b Remove dead code and group CLI root commands
```

The after run must use the same scenario set, same model configuration, same
cache mode, same soak server mode, and same latency ceilings. If any of those
inputs change, the report is not a clean before/after comparison.

## Attribution Standard

ABC gains or losses may only be claimed when the comparison is valid for
attribution. A paired run is valid only when both lanes use the same:

- scenario set
- cache settings
- server mode
- session isolation mode
- request timeout and latency ceiling
- effective model/provider configuration
- observed model set

If any of those inputs differ, the result may still be useful diagnostic
evidence, but it is not ABC-attributable evidence.

Every claimed ABC gain or loss must identify both:

- the user-visible behavior delta, such as pass/fail movement or changed
  scenario checks
- the matching contract/RCA delta, such as severity, phase, recovery action,
  recovery outcome, confidence effect, or retry suppression

Behavior-only changes are not enough to claim ABC causality. Contract-only
changes are not enough to claim user-visible improvement. The useful release
claim is the intersection: behavior changed and ABC contract evidence explains
why.

## Required Lanes

Run two paired lanes:

1. `abc-baseline`: current pre-aggregation v1.0.8 branch state.
2. `abc-after`: branch state after the remaining ABC contract aggregation work.

Both lanes should use:

```bash
SOAK_SERVER_MODE=clone
SOAK_CLEAR_CACHE=1
SOAK_BYPASS_CACHE=1
SOAK_SESSION_ISOLATION=1
SOAK_SCENARIOS=acknowledgement_sla,introspection_discovery,tool_random_use,model_identity,delegation,filesystem_access_denial,current_events,quote_safety
```

If local model latency is being evaluated, the same lane may be repeated with a
known local model and a higher loop ceiling:

```bash
SOAK_AUTONOMY_MAX_LOOP_SECS=600
```

The local-model lane is diagnostic evidence, not a substitute for the main
paired soak.

## Evidence To Capture

Each lane must preserve:

- behavior soak JSON report
- retained `state.db` snapshot from the isolated run
- daemon log
- current git commit
- model/provider configuration summary
- scenario list and cache flags
- RCA query output for each scenario session

The RCA extraction must summarize every `guard_contract_evaluated` and
`verifier_contract_evaluated` event for the scenario sessions:

- total contract events
- hard / soft / neutral counts
- retry / record / suppress / block counts
- recovery window count and recovery outcome
- confidence-effect counts: `-1`, `0`, `1`
- phase counts across Retrieve, Think, Execute, Observe, Reflect, Remember
- top contract ids

## Pass Criteria

The after lane is acceptable only if:

- no baseline-passing behavior scenario regresses to failure
- no canned fallback text is introduced
- false capability-denial markers do not increase
- empty/no-answer outcomes do not increase
- hard contract violations are visible in RCA when they happen
- soft contract violations show recovery or recorded degradation instead of
  hidden retries
- contract events include phase, severity, recovery action, recovery outcome,
  and confidence effect
- memory absence remains neutral unless contradiction evidence exists

## Improvement Criteria

The after lane is better only if at least one of the following improves without
regression:

- fewer unsupported certainty or false access-denial failures
- fewer unnecessary retries after useful observed work
- clearer primary RCA diagnosis for recovered guard/verifier failures
- better contract event coverage for failed/degraded rows
- lower recovery attempts per successful scenario

## Non-Goals

- Do not require every scenario to have a contract violation. Clean scenarios
  should remain clean.
- Do not treat more guard activity as improvement by itself.
- Do not tune model prose to satisfy the soak.
- Do not compare different models as if that were an ABC before/after result.

## Manual Commands

Create an isolated baseline worktree so the release branch is not detached:

```bash
git worktree add /tmp/roboticus-abc-baseline b20bb69b
cd /tmp/roboticus-abc-baseline
SOAK_SERVER_MODE=clone SOAK_CLEAR_CACHE=1 SOAK_BYPASS_CACHE=1 SOAK_SESSION_ISOLATION=1 \
  SOAK_REPORT_PATH=/tmp/roboticus-abc-baseline.json \
  SOAK_SCENARIOS=acknowledgement_sla,introspection_discovery,tool_random_use,model_identity,delegation,filesystem_access_denial,current_events,quote_safety \
  python3 scripts/run-agent-behavior-soak.py
```

Run the after lane from the active release branch:

```bash
cd /Users/jmachen/code/roboticus
SOAK_SERVER_MODE=clone SOAK_CLEAR_CACHE=1 SOAK_BYPASS_CACHE=1 SOAK_SESSION_ISOLATION=1 \
  SOAK_REPORT_PATH=/tmp/roboticus-abc-after.json \
  SOAK_SCENARIOS=acknowledgement_sla,introspection_discovery,tool_random_use,model_identity,delegation,filesystem_access_denial,current_events,quote_safety \
  python3 scripts/run-agent-behavior-soak.py
```

The behavior soak runner writes a retained database snapshot next to each
report by default: `<report>.state.db`. Generate the ABC evidence comparison:

```bash
python3 scripts/abc_contract_soak_report.py \
  --baseline-report /tmp/roboticus-abc-baseline.json \
  --after-report /tmp/roboticus-abc-after.json \
  --output /tmp/roboticus-abc-contract-comparison.json
```

The behavior reports alone are insufficient. The release evidence is the pair
of behavior reports plus the retained state database snapshots and the
contract comparison JSON. The comparison output must say
`valid_for_abc_attribution: true` before any gain or loss is described as caused
by the ABC changes.
