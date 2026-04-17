# System 13: Channel Adapter Behavior

## Status

- Owner: parity-forensics program
- Audit status: `in progress`
- Last updated: 2026-04-17
- Related release: v1.0.6

## Why This System Matters

Connectors are supposed to be thin, but that does not mean channel semantics are
irrelevant. If behavior diverges by transport, the architecture can remain
structurally clean while still violating the operator contract.

## Scope

In scope:

- channel-specific parsing and formatting
- transport-preserved metadata
- consent / sender / chat identity propagation
- connector-specific behavior that can alter the effective pipeline input/output

Out of scope:

- core pipeline behavior shared across all channels

## Rust Source Anchors

| Concern | Rust file(s) / function(s) |
|---------|-----------------------------|
| Channel adapters | `src/.../channel*` |

## Go Source Anchors

| Concern | Go file(s) / function(s) |
|---------|---------------------------|
| Channel router / normalization envelope | `internal/channel/router.go`, `internal/channel/adapter.go` |
| Outbound formatting | `internal/channel/formatter.go` |
| Adapter-specific parsing | `internal/channel/telegram.go`, `discord.go`, `signal.go`, `whatsapp.go`, `email.go`, `matrix.go`, `a2a.go`, `web.go` |
| Webhook route entrypoints | `internal/api/routes/admin_webhooks.go` |
| CLI / admin surfaces | `cmd/channels/channels.go`, `internal/api/routes/admin.go` |

## Live Go Path

Each adapter translates an external protocol into `pipeline.Input` and formats
`Outcome` back out. The audit target is transport semantics, not shared
business logic.

The current Go code already enforces connector thinness structurally, but the
transport contract still depends on what metadata and media each adapter or
route preserves when constructing the normalized inbound message.

## Artifact Boundary

- canonical `pipeline.Input` produced by each transport
- channel-visible output shape

## Success Criteria

- Closure artifact(s):
  - normalized pipeline input per channel
  - formatted output per channel
- Live-path proof:
  - integration tests prove that connectors stay thin while preserving the
    right user/session metadata
- Blocking conditions:
  - channel-specific behavior exists but is undocumented
  - connectors mutate shared behavior instead of only translating it
- Accepted deviations:
  - transport-specific formatting is allowed if the underlying behavioral
    contract remains equivalent

## Divergence Register

| ID | Priority | Concern | Rust behavior | Go behavior | Classification | Status | Evidence |
|----|----------|---------|---------------|-------------|----------------|--------|----------|
| SYS-13-001 | P1 | Webhook normalization is split between route handlers and adapters | Rust channel ingress ownership needs explicit comparison | Telegram and WhatsApp webhook routes construct `pipeline.Input` directly, while the adapters themselves also implement webhook parse/normalize paths (`ProcessWebhook(...)`) | Degradation / split ownership | Open | `internal/api/routes/admin_webhooks.go`, `internal/channel/telegram.go`, `internal/channel/whatsapp.go` |
| SYS-13-002 | P2 | Router remains structurally thin and formatting-owned, not behavior-owned | Rust connector thinness intent | `channel.Router` only polls adapters, formats outbound content per platform, and manages delivery/health state; it does not own business decisions | Idiomatic shift / accepted | Accepted | `internal/channel/router.go`, `internal/channel/formatter.go` |
| SYS-13-003 | P2 | Transport metadata preservation differs by adapter and needs explicit classification | Rust per-channel metadata mapping needs comparison | Adapters preserve different normalized fields: e.g. Signal group messages become `group:<id>` chat IDs, WhatsApp surfaces media attachments, Matrix preserves sender IDs and room IDs, Telegram may omit sender ID in some cases | Open | Open | `internal/channel/signal.go`, `internal/channel/whatsapp.go`, `internal/channel/matrix.go`, `internal/channel/telegram.go`, coverage tests |
| SYS-13-004 | P2 | Outbound formatting is richer and centralized | Rust formatter behavior needs comparison | `FormatFor(platform)` strips internal orchestration metadata and converts markdown to platform-native syntax before send | Likely improvement | Accepted | `internal/channel/formatter.go`, `internal/channel/formatter_parity_test.go` |

## Intentional Deviations

- Go may keep different adapter ergonomics if the normalized `pipeline.Input`
  and user-visible contract remain equivalent.
- Platform-specific formatting differences are acceptable if they stay
  translation-only and do not change the underlying behavioral result.

## Remediation Notes

Promoted from an implicit concern. Actual behavior drift has now been found at
the ingress normalization seam, so this system is no longer only precautionary.

## Downstream Systems Affected

- System 01: request construction
- System 07: service/config lifecycle
- System 09: observability

## Open Questions

- Which channel surfaces are truly behaviorally distinct enough to require their
  own sub-audits?
- Should webhook routes delegate to adapters for normalization, or should the
  route-owned normalization path become the single explicit ingress contract?

## Progress Log

- 2026-04-17: Initialized cross-cutting system document.
- 2026-04-17: Deepened with a concrete split-ingress finding: some channels
  normalize webhooks in routes while adapters also implement their own parse
  path.
