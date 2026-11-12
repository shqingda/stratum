# Recorded Responses fixtures

18 fixtures recorded on 2026-11-12 using Talos's adapters and the user's subscription
accounts. Request headers were never recorded. `scrub.go` removes credentials,
account/organization/user identifiers and email addresses before fixtures enter
this directory. SSE framing and other response bytes are retained.

Each fixture has the actual HTTP request body (`.req.json`), the response body
(`.sse`, including JSON error responses), and the HTTP status (`.status`).
`../fixtures_test.go` reconstructs the same `Request`, compares the complete
request body, and replays every response without external network access. The
pre-fix Codex limit recording has one explicitly checked difference, described below.

| Provider | Model | Fixtures |
| --- | --- | --- |
| Codex | gpt-6-luna | text, thinking, tool, tools-parallel, tool-result, image, error-model, cancel, max-tokens, max-tokens-unsupported |
| OpenAI / Sign in with ChatGPT | gpt-6-luna | text, tool |
| OpenAI / Sign in with ChatGPT | gpt-5-nano | model-unavailable |
| xAI / X subscription | grok-build-0.1 | text, tool, thinking-default, effort-unsupported |
| xAI / X subscription | grok-4.7 | thinking-low |

The models were selected from `talosd models`: gpt-6-luna is the least expensive
Codex model, and grok-build-0.1 is the least expensive xAI model. OpenAI's cheaper
gpt-5-nano was rejected for the ChatGPT subscription, so its successful samples
use gpt-6-luna. The rejected request is retained as model-unavailable.
The user separately authorized trying the more expensive grok-4.7 with low effort.
That additional request succeeded and is retained as thinking-low, with a real
reasoning summary. It is not a high-effort fixture.

## Codex output-limit correction

The historical max-tokens-unsupported fixture was recorded before the adapter fix:
`codexBody` deleted `max_output_tokens` even though `Request.MaxTokens` was 16.
The reply used 214 output tokens and ended normally. This proved that the adapter
ignored the limit; it did not prove that the server rejected it. The original
request and response remain unchanged, and replay still requires `StopEnd` and
output usage greater than 16. Request comparison first asserts that the recording
lacks `max_output_tokens`, adds 16 only to its in-memory expected copy, and then
compares all fields with the current request.

The new max-tokens fixture comes from one diagnostic request to
`https://chatgpt.com/backend-api/codex/responses`: a temporary transport added
`max_output_tokens=16` before recording and sending the actual request. The server
returned HTTP 200, `response.incomplete` with reason `max_output_tokens`, and usage
of 33 input / 16 output tokens. The existing parser produced `StopMaxTokens`.
The adapter now preserves the requested limit, and this recording is replayed with
the identical `Request` and complete body comparison. No response bytes were fabricated.

## Incomplete task requirements

These fixtures do **not** complete task 1:

- grok-build-0.1 rejects `reasoning.effort=high` with HTTP 400. That response is
  effort-unsupported. thinking-default uses the same reasoning question without
  an effort parameter and contains a real reasoning summary; it does not prove
  high-effort support.

The required xAI high-effort thinking fixture remains absent.
No synthetic data was substituted for it.
The cancel fixture ends after received text and intentionally has no final usage.
Tool-only replies can contain no text; their tool calls and usage are asserted.

## Recording and checking

`talosd chat --record <private-directory> ...` records numbered files while the
adapter reads the stream. Finish the request before scrubbing: the scrubber uses
atomic file replacement, which must not run against a file still being recorded.
The exact requests for the scenarios above are in `fixtureRequest` in the tests;
image uses a generated 32 by 32 red PNG, and tool-result uses the previous recorded
tool reply plus a fixed, non-private tool result.

From the repository root:

```sh
go run llm/testdata/scrub.go <finished-recording-directory>
go run llm/testdata/scrub.go -check llm/testdata
go test ./llm -run 'TestFixtures|TestScrubFixtures'
```

The recording run made 21 counted account HTTP requests: 19 model requests and
2 OAuth refreshes. One initial Codex limit capture was discarded after it was
scrubbed before recording finished; the replacement was recorded completely.
No Claude/Anthropic account was accessed.
