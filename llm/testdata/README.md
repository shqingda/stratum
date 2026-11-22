# Recorded Responses fixtures

19 fixtures recorded on 2026-11-12 using Stratum's adapters (then named Talos) and the user's subscription
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
| xAI / X subscription | grok-4.7 | thinking-low, thinking |

The models were selected from `stratumd models`: gpt-6-luna is the least expensive
Codex model, and grok-build-0.1 is the least expensive xAI model. OpenAI's cheaper
gpt-5-nano was rejected for the ChatGPT subscription, so its successful samples
use gpt-6-luna. The rejected request is retained as model-unavailable.
The user separately authorized trying the more expensive grok-4.7 with low effort.
That additional request succeeded and is retained as thinking-low, with a real
reasoning summary. The user then authorized the remaining high-effort test and
asked to conserve quota. One grok-4.7/high request succeeded and is retained as
thinking; it completes the required xAI high-effort coverage.

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

## xAI high-effort completion

The thinking fixture uses grok-4.7, high effort, the same short reasoning question,
and MaxTokens=1024. [xAI documents this limit](https://docs.x.ai/developers/model-capabilities/text/best-practices)
as applying to visible output rather than a separate reasoning-token budget.
The recording helper allowed exactly one
model request, no retries or redirects, and a 60-second timeout. The request
succeeded without an OAuth refresh: HTTP 200, response.completed, a nonempty
reasoning summary, and StopEnd. Usage was 1288 input tokens (1152 cached), 429
output tokens (326 reasoning), and 1717 total tokens.

All task 1 scenarios are now represented by real recordings. The cheaper
model's effort-unsupported fixture remains unchanged: grok-build-0.1 rejected
reasoning.effort=high with HTTP 400. thinking-default and thinking-low remain
separate coverage; neither is relabeled as high. The cancel fixture intentionally
ends after received text without final usage. Tool-only replies may have no text;
their tool calls and usage are asserted.

## Recording and checking

`stratumd chat --record <private-directory> ...` records numbered files while the
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

The recording run made 22 counted account HTTP requests: 20 model requests and
2 OAuth refreshes. One initial Codex limit capture was discarded after it was
scrubbed before recording finished; the replacement was recorded completely.
No Claude/Anthropic account was accessed.
