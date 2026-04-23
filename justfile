# Roboticus — local tasks aligned with `.github/workflows/ci.yml`
#
# CI uses golangci-lint v2.11.4 (see workflow). Install: https://golangci-lint.run/welcome/install/
# Parity audit is a local/release helper, not part of CI.
#
# Requires: Go (version from go.mod), golangci-lint, govulncheck (`go install golang.org/x/vuln/cmd/govulncheck@latest`)

set shell := ["bash", "-euo", "pipefail", "-c"]

default:
    @just --list

# Replicates the GitHub Actions CI workflow (sequential; jobs run in parallel on CI).
ci-test: lint test smoke fuzz-ci soak-fuzz architecture build-ci security

# Back-compat alias for the shorter historical recipe name.
ci: ci-test

# Local/release superset that also includes the parity audit.
ci-plus: ci-test parity-audit

# --- CI stages (also usable standalone) ---

lint:
    go vet ./...
    golangci-lint run ./...

test:
    go test -race -coverprofile=coverage.out -covermode=atomic -timeout 20m ./...

# Same flags as CI `test` job; writes coverage.out + browsable coverage.html and prints total %.
coverage:
    #!/usr/bin/env bash
    set -euo pipefail
    go test -race -coverprofile=coverage.out -covermode=atomic -timeout 20m ./...
    go tool cover -html=coverage.out -o coverage.html
    echo ""
    echo "coverage.html (open in a browser)"
    go tool cover -func=coverage.out | tail -1

smoke:
    go test -v -run TestLiveSmokeTest -timeout 120s .

architecture:
    go test -v -run TestArchitecture ./internal/api/

# Short fuzz run (10s per target), matching the `fuzz` job in ci.yml
fuzz-ci:
    #!/usr/bin/env bash
    set -euo pipefail
    go test -run='^$' -fuzz=FuzzInjectionDetector_CheckInput -fuzztime=10s ./internal/agent/
    go test -run='^$' -fuzz=FuzzInjectionDetector_Sanitize -fuzztime=10s ./internal/agent/
    go test -run='^$' -fuzz=FuzzTelegramFormatter -fuzztime=10s ./internal/channel/
    go test -run='^$' -fuzz=FuzzSignalFormatter -fuzztime=10s ./internal/channel/
    go test -run='^$' -fuzz=FuzzWhatsAppFormatter -fuzztime=10s ./internal/channel/
    go test -run='^$' -fuzz=FuzzValidateE164 -fuzztime=10s ./internal/channel/
    go test -run='^$' -fuzz=FuzzIsValidCronExpression -fuzztime=10s ./internal/schedule/
    go test -run='^$' -fuzz=FuzzMatchesCron -fuzztime=10s ./internal/schedule/

parity-audit:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ -n "${SKIP_PARITY:-}" ]]; then
        echo "Skipping parity audit (SKIP_PARITY is set)"
        exit 0
    fi
    if [[ ! -d _roboticus ]]; then
        echo "error: parity audit needs ./_roboticus (clone robot-accomplice/roboticus) or set SKIP_PARITY=1" >&2
        exit 1
    fi
    go run . parity-audit --roboticus-dir=_roboticus

# Soak + longer fuzz window; matches `soak-fuzz` job env in ci.yml
soak-fuzz:
    SOAK_ROUNDS=2 FUZZ_SECONDS=20 bash scripts/run-soak-fuzz.sh

# Static linux/amd64 binary + version check; matches `build` job in ci.yml
build-ci:
    #!/usr/bin/env bash
    set -euo pipefail
    commit_sha="$(git rev-parse HEAD)"
    export CGO_ENABLED=0 GOOS=linux GOARCH=amd64
    go build -trimpath \
        -ldflags="-s -w -X roboticus/cmd/internal/cmdutil.Version=ci-${commit_sha:0:8} -X roboticus/internal/daemon.version=ci-${commit_sha:0:8}" \
        -o roboticus .
    if [[ "$(uname -s)" == "Linux" && "$(uname -m)" == "x86_64" ]]; then
        ./roboticus version
    else
        file roboticus
    fi

security:
    govulncheck ./...

# --- Common development shortcuts ---

run-source log_level="INFO":
    ROBOTICUS_LOG_LEVEL={{log_level}} go run . serve

build:
    go build ./...

vet:
    go vet ./...

fmt:
    gofmt -w .

check: build vet test

clean:
    rm -f coverage.out coverage.html roboticus parity-report.md
