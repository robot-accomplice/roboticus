// v1.0.4 Changelog entry for roboticus-site
// Copy this object into roboticus-site/src/lib/changelog-updates.ts

export const v104 = {
  version: "1.0.4",
  date: "2026-04-14",
  geek: `Security-first hardening: Store.DB() removed, wallet keystore-only, delivery queue recovery. Pipeline refactored to 16 named stages. 26-guard chain with FinancialActionTruthGuard. Session-aware model escalation. Topic-aware compression. Cache guards for unparsed tool calls. Two new apps (Eastern/Western Philosophy), two new plugins (Codex CLI, Web Research). Dashboard settings overhaul (16 sections, 303-field schema). Theme texture registry with site-hosted images. cmd/ split into 12 subpackages. Soak-appropriate test timeouts. 186 files changed, +9,240/-3,032.`,
  layman: `Major security and stability update. The agent is now smarter about switching to better AI models when it detects problems mid-conversation. Two new philosophy personalities (Eastern and Western) are available as app profiles. The dashboard settings page has been completely redesigned to be more intuitive. Theme textures now display correctly with real images. All 32 planned improvements shipped — nothing deferred.`,
  breaking: [
    "Store.DB() removed — use Store query methods instead",
    "ROBOTICUS_WALLET_PASSPHRASE env var no longer supported — use keystore",
    "security.workspace_only (top-level) deprecated — use security.filesystem.workspace_only",
    "APIKeyEnv, TokenEnv, PasswordEnv config fields removed — use keystore",
  ],
};
