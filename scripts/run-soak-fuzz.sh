#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────
# run-soak-fuzz.sh — Deterministic soak + bounded fuzzing for Roboticus
#
# Runs specific test cases in a loop to surface flaky behavior,
# then exercises Go's native fuzzer on security-critical inputs.
#
# Usage:
#   bash scripts/run-soak-fuzz.sh
#   SOAK_ROUNDS=10 FUZZ_SECONDS=60 bash scripts/run-soak-fuzz.sh
# ─────────────────────────────────────────────────────────────────
set -euo pipefail

SOAK_ROUNDS="${SOAK_ROUNDS:-5}"
FUZZ_SECONDS="${FUZZ_SECONDS:-45}"

echo "Soak/fuzz run starting (rounds=${SOAK_ROUNDS}, fuzz_seconds=${FUZZ_SECONDS})"

# ── Stage 1: Deterministic soak loops ────────────────────────────
echo ""
echo "1) Deterministic soak loops"
for i in $(seq 1 "$SOAK_ROUNDS"); do
  echo "  - soak round ${i}/${SOAK_ROUNDS}"
  go test -run TestArchitecture ./internal/api/
  go test -run TestLiveSmokeTest -timeout 120s .
done

# ── Stage 2: Bounded fuzz targets ────────────────────────────────
echo ""
echo "2) Bounded fuzz targets (${FUZZ_SECONDS}s per target)"

FUZZ_TARGETS=(
  "FuzzInjectionDetector_CheckInput:./internal/agent/"
  "FuzzInjectionDetector_Sanitize:./internal/agent/"
  "FuzzTelegramFormatter:./internal/channel/"
  "FuzzSignalFormatter:./internal/channel/"
  "FuzzWhatsAppFormatter:./internal/channel/"
  "FuzzValidateE164:./internal/channel/"
  "FuzzIsValidCronExpression:./internal/schedule/"
  "FuzzMatchesCron:./internal/schedule/"
)

for target_spec in "${FUZZ_TARGETS[@]}"; do
  IFS=: read -r target pkg <<< "$target_spec"
  echo "  - fuzzing ${target} in ${pkg} for ${FUZZ_SECONDS}s"
  go test -fuzz="${target}" -fuzztime="${FUZZ_SECONDS}s" "${pkg}"
done

echo ""
echo "Soak/fuzz battery PASSED"
