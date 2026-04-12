# Settings Page Overhaul — Handoff Document

**Status**: Incomplete. Backend schema endpoint works. Frontend rendering is broken/insufficient.
**Priority**: Must complete before v1.0.3 release.

---

## What Exists

### Backend (working)
- `GET /api/config/schema` — returns 303 fields from Config struct via reflect
- Each field: `name` (dotted path), `type`, `default`, `current`, `section`, `description`, `enum`, `immutable`
- File: `internal/api/routes/config_schema.go`
- Test: `internal/api/routes/config_schema_test.go`
- Route registered at `internal/api/server.go:280`

### Frontend (broken)
- The settings renderer in `dashboard_spa.html` was supposed to fetch `/api/config/schema` and drive the form from it
- The tooltip system (`showSettingsTooltip`, `fieldTooltips`, `tooltipSpan`) was added but reportedly not working — clicks on `(?)` do nothing
- The dynamic rendering produces a messy layout with inconsistent field widths and random section ordering

---

## What's Wrong (User Feedback)

1. **Not struct-complete**: Not all Config struct fields are visible in the UI
2. **Tooltips broken**: Question mark cursor shows but clicking does nothing
3. **Layout is terrible**: Field and label lengths inconsistent, looks hastily generated. The page should look deliberately designed since we know every field in advance
4. **Section order random**: Sections appear in whatever order reflect returns them, not a logical user-facing order

---

## What Needs to Happen

### Approach: Hand-Crafted Sections, Schema-Validated

Do NOT dynamically generate the settings form from schema data. Instead:

1. **Hand-craft each section** in the HTML with proper layout, fixed label widths, consistent field sizing, and logical grouping
2. **Use the schema endpoint** only for: populating current values, providing defaults as placeholders, validating on save, and ensuring no field is missed
3. **Match the Rust settings page** at `/Users/jmachen/code/roboticus-rust/crates/roboticus-api/src/dashboard/js/pages/settings.js`

### Section Order (fixed, not dynamic)

1. Agent & Identity (name, id, workspace, delegation, composition)
2. Models & Routing (primary, fallbacks, routing mode, auto ordering)
3. Security (scope_mode, allowlists, authorities, sandbox, filesystem)
4. Memory (budget percentages, embedding, search, decay)
5. Context Budget (L0-L3 tiers, soul_max_context_pct)
6. Session (scope_mode, ttl, reset)
7. Cache (enabled, min_tokens, ttl)
8. Channels (per-channel subsections: telegram, discord, signal, email, whatsapp, web)
9. Matrix (homeserver, user_id, rooms, encryption)
10. MCP Servers (add/edit/delete/test — management UI already built)
11. Plugins (dir, allow/deny, strict_permissions)
12. Skills (script runtime settings)
13. Server (port, bind, rate limits — immutable, restart required)
14. Wallet (path, chain_id, rpc_url — immutable)
15. Treasury (budget caps, reserves)
16. Advanced/Rarely Changed (abuse, approvals, backups, cors, daemon, dkim, heartbeat, etc.)

### Layout Principles

- **Label column**: fixed 200px width, right-aligned, muted color
- **Input column**: consistent width per type (text: 100%, number: 120px, toggle: 40px, enum: 200px)
- **Section cards**: each section in a card with a title header
- **Immutable fields**: disabled with "requires restart" badge
- **Arrays**: rendered as tag-input or newline-separated textarea
- **Nested objects**: indented sub-section within the parent card

### Tooltip Fix

The `showSettingsTooltip` function exists (line ~5521) but the `tooltipSpan` function that generates `(?)` elements may not be called during rendering. Verify:
1. The settings form renderer calls `tooltipSpan(key)` for each field
2. The `onclick` handler correctly invokes `showSettingsTooltip`
3. The popover CSS (`.settings-tooltip-popover`) positions correctly relative to the trigger
4. `fieldTooltips` map has entries for all visible fields

### Rust Reference

Read `/Users/jmachen/code/roboticus-rust/crates/roboticus-api/src/dashboard/js/pages/settings.js` for:
- Section ordering
- Field rendering patterns
- Enum field handling
- Access Control tab layout
- Model Order tab (drag-and-drop)
- Channel subsection patterns
- Immutable section handling

### Testing Checklist

After implementation:
- [ ] Every Config struct field has a corresponding UI control
- [ ] Clicking (?) shows a tooltip popover with description
- [ ] Sections appear in the fixed order above
- [ ] Label widths are consistent across all sections
- [ ] Enum fields render as dropdowns
- [ ] Boolean fields render as toggles
- [ ] Immutable fields are disabled with restart badge
- [ ] Save/Apply persists changes without errors
- [ ] New fields added to Config struct automatically appear (via schema diff check)

---

## Workspace Animation Jerkiness

Separate issue noted during UAT. The workspace canvas animations appear jerky. Possible causes:
- The `update(dt)` timestep clamping at `Math.min((ts - self.lastTime) / 1000, 0.05)` may be too aggressive
- The `resize()` being called too frequently via ResizeObserver
- Theme color parsing (`parseThemeColors`) running every frame — involves DOM reads and a throwaway canvas
- The Rust version uses `bot.role === 'agent'` for sizing but Go uses `bot.role === 'orchestrator' || bot.role === 'agent'` — verify the sizing values match

Fix: cache `parseThemeColors` result and only re-parse on theme change, not every frame.

---

## Files to Modify

- `internal/api/dashboard_spa.html` — settings renderer (primary work)
- `internal/api/routes/config_schema.go` — may need section ordering metadata
- No backend changes needed — schema endpoint is complete
