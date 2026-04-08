# Profile: P07 — Tool Call Parsing

## Status: PROFILED (Wave 1)

## Files Covered
- roboticus-pipeline/src/tool_parsing.rs (215 lines)
- roboticus-llm/src/format.rs (tool-call-related response parsing)

## Functions

### parse_tool_call(response: &str) -> Option<(String, Value)>
- Backward search from end of response for `"tool_call"` marker
- Finds preceding `{`, counts braces forward to find matching `}`
- Returns LAST valid tool call found (single result)
- No truncation recovery

### parse_tool_calls(response: &str) -> Vec<(String, Value)>
- Forward search from start for all `"tool_call"` markers
- Finds ALL valid tool calls in order of appearance
- **Truncation recovery**: if brace counting reaches end of string without closing, appends N closing braces (N = depth) and retries parse
- Stops searching after truncation recovery (break)
- False positive rejection: skips if `}` exists between `{` and `"tool_call"`

### extract_tool_invocation(parsed: &Value) -> Option<(String, Value)>
- Private helper
- Name field priority: `name` > `tool_name` > `tool`
- Params field priority: `params` > `arguments` > `args` > `input` (checked in tool_call object, then top-level)
- Supports shorthand: `{"tool_call": "bash", "params": {...}}`
- Missing params defaults to `{}`

### classify_provider_error(raw: &str) -> &'static str
- Case-insensitive pattern matching (8 categories)
- circuit breaker → "provider temporarily unavailable"
- 401/403/authentication → "provider authentication error"
- 429/rate limit → "provider rate limit reached"
- 402/quota/billing/credit → "provider quota or billing issue"
- 500-504 → "provider server error"
- timeout/connection → "network error reaching provider"
- fallback → "provider error"

### provider_failure_user_message(last_error, message_already_stored) -> String
- Formats user-facing error with category + optional timeout hint
- Two variants: stored (will retry) vs not stored (please retry)

### extract_timeout_hint(raw: &str) -> String
- Private helper parsing "configured limit: key = value" from error text
- Strips "models.routing." prefix for readability
