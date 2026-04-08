# Profile: P01 — LLM Format/Client/Embedding/Transform/Compression

## Status: PROFILED (Wave 1)

## Files Covered
- roboticus-llm/src/format.rs (2075 lines) — request marshaling, response parsing, SSE, multimodal
- roboticus-llm/src/client.rs (705 lines) — HTTP transport, x402 payment, auth modes
- roboticus-llm/src/embedding.rs (735 lines) — provider embedding, n-gram fallback
- roboticus-llm/src/transform.rs (366 lines) — reasoning extraction, format normalization, injection filter
- roboticus-llm/src/compression.rs — entropy-based token importance scoring

## Key Constants
| Name | Value | Purpose |
|------|-------|---------|
| REQUEST_TIMEOUT | 120s | HTTP request timeout |
| CONNECT_TIMEOUT | 10s | TCP connection timeout |
| X402_MAX_AUTO_PAY_USDC | 1.0 | Safety rail for payment |
| DEFAULT_MAX_RESPONSE_BYTES | 2 MiB | Stream accumulator limit |
| NGRAM_DIM | 128 | Default embedding dimension |
| EMBED_TIMEOUT | 30s | Embedding API timeout |
| INJECTION_MARKERS | 5 strings | [SYSTEM], [INST], <|im_start|>, <s>, </s> |

## Provider Request Formats (exact JSON field names)

### Anthropic
- `messages` array (excludes system role), `system` top-level, `max_tokens`, tools use `input_schema`

### OpenAI Completions
- `messages` array (includes system), `max_tokens`, `temperature`, tools wrapped in `{"type":"function","function":{...}}` with `parameters`

### OpenAI Responses
- `input` array (not messages), `max_output_tokens` (not max_tokens), content blocks typed as `input_text`, tools flat `{"type":"function","name":...,"parameters":...}`

### Google Generative AI
- `contents` array, role "model" (not "assistant"), `systemInstruction` top-level, `maxOutputTokens` camelCase, tools in `functionDeclarations`

### Ollama
- Standard `messages` array, `options.temperature`

## Tool Call Shim Format (universal)
All providers normalize tool calls to: `{"tool_call":{"name":"...","params":{...}}}` embedded as JSON text in content string.

## Response Parsing
- Anthropic: `content[].type=="tool_use"` → extract name + input
- OpenAI: `tool_calls[].function` → name + arguments (string OR object for Kimi K2)
- OpenAI Responses: `output[].type=="function_call"` → name + arguments
- Google: `parts[].functionCall` → name + args (already object)

## N-gram Embedding Fallback
- Character 3-gram hashing: `acc = (acc * 31) + char_as_u32`
- Bucket: `hash % 128`
- L2 normalized to unit vector
- Text < 3 chars returns zero vector

## Transform Pipeline (default order)
1. ReasoningExtractor — strips `<think>...</think>` blocks
2. FormatNormalizer — trims, collapses 3+ newlines to 2, strips single wrapping code fence
3. ContentGuard — filters 5 injection markers, replaces with "[Content filtered for safety]"

## Compression
- Token estimation: `split_whitespace().count()` (word-based)
- Scoring: content words +3.0, stop words +0.5, code punctuation +2.0, capitalized +1.0, digits +1.5, position bias (first/last 10%) +1.0
- 63 stop words in set
- Target ratio clamped to [0.1, 1.0]

Full behavioral detail in agent session context (too large for single file).
