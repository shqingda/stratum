package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestFixtures(t *testing.T) {
	paths, err := filepath.Glob("testdata/*/*.sse")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"codex/cancel", "codex/error-model", "codex/image", "codex/max-tokens", "codex/max-tokens-unsupported",
		"codex/text", "codex/thinking", "codex/tool", "codex/tool-result", "codex/tools-parallel",
		"openai/model-unavailable", "openai/text", "openai/tool",
		"xai/effort-unsupported", "xai/text", "xai/thinking", "xai/thinking-default", "xai/thinking-low", "xai/tool",
	}
	for i, name := range want {
		want[i] = filepath.Join("testdata", filepath.FromSlash(name)) + ".sse"
	}
	sort.Strings(want)
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("fixture inventory changed:\ngot %v\nwant %v", paths, want)
	}
	for _, path := range paths {
		provider := filepath.Base(filepath.Dir(path))
		scenario := strings.TrimSuffix(filepath.Base(path), ".sse")
		t.Run(provider+"/"+scenario, func(t *testing.T) {
			req := fixtureRequest(t, provider, scenario)
			r, err := replayFixture(t, provider, scenario, req)
			switch scenario {
			case "error-model", "model-unavailable", "effort-unsupported":
				var modelErr *Error
				if !errors.As(err, &modelErr) || modelErr.Retryable {
					t.Fatalf("want non-retryable model error, got %v", err)
				}
				return
			case "cancel":
				if !Retryable(err) || r.Stop != "" || r.Message.PlainText() == "" {
					t.Fatalf("want partial text and an unfinished stream, got stop=%q text=%q err=%v", r.Stop, r.Message.PlainText(), err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if r.Usage.Input <= 0 || r.Usage.Output <= 0 {
				t.Errorf("missing usage: %+v", r.Usage)
			}
			wantStop := StopEnd
			var wantPaths []string
			switch scenario {
			case "tool":
				wantStop, wantPaths = StopToolUse, []string{"hello.txt"}
			case "tools-parallel":
				wantStop, wantPaths = StopToolUse, []string{"alpha.txt", "beta.txt"}
			case "max-tokens":
				wantStop = StopMaxTokens
				if r.Usage.Output != req.MaxTokens {
					t.Errorf("output tokens = %d, want recorded limit %d", r.Usage.Output, req.MaxTokens)
				}
			case "max-tokens-unsupported":
				// Before the fix, codexBody deleted the limit from this recorded request.
				if provider != "codex" || r.Usage.Output <= req.MaxTokens {
					t.Fatalf("want evidence of the Codex adapter ignoring the output limit, got %+v", r.Usage)
				}
			}
			if r.Stop != wantStop {
				t.Errorf("stop = %q, want %q", r.Stop, wantStop)
			}
			var gotPaths []string
			calls := r.Message.ToolCalls()
			if len(calls) != len(wantPaths) {
				t.Fatalf("tool calls = %d, want %d", len(calls), len(wantPaths))
			}
			for _, call := range calls {
				var args struct{ Path string }
				if call.Name != "read_file" || call.ID == "" || !json.Valid(call.Args) || json.Unmarshal(call.Args, &args) != nil {
					t.Fatalf("invalid tool call: %+v", call)
				}
				gotPaths = append(gotPaths, args.Path)
			}
			sort.Strings(gotPaths)
			if !reflect.DeepEqual(gotPaths, wantPaths) {
				t.Errorf("tool paths = %v, want %v", gotPaths, wantPaths)
			}
			if len(calls) == 0 && strings.TrimSpace(r.Message.PlainText()) == "" {
				t.Error("empty answer")
			}
			if scenario == "thinking" || scenario == "thinking-default" || scenario == "thinking-low" {
				var summary strings.Builder
				for _, part := range r.Message.Parts {
					if part.Thinking != nil {
						summary.WriteString(part.Thinking.Text)
					}
				}
				if strings.TrimSpace(summary.String()) == "" {
					t.Error("empty reasoning summary")
				}
			}
		})
	}
}

func replayFixture(t *testing.T, provider, scenario string, req Request) (Reply, error) {
	t.Helper()
	prefix := filepath.Join("testdata", provider, scenario)
	read := func(suffix string) []byte {
		b, err := os.ReadFile(prefix + suffix)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	status, err := strconv.Atoi(strings.TrimSpace(string(read(".status"))))
	if err != nil {
		t.Fatal(err)
	}
	srv, got := replay(t, status, string(read(".sse")))
	p := &OpenAI{BaseURL: srv.URL, Auth: StaticHeader("Authorization", "Bearer fixture"), Codex: provider == "codex"}
	r, streamErr := Collect(p.Stream(context.Background(), req), nil)
	var want map[string]any
	if err := json.Unmarshal(read(".req.json"), &want); err != nil {
		t.Fatal(err)
	}
	if provider == "codex" && scenario == "max-tokens-unsupported" {
		if _, exists := want["max_output_tokens"]; exists {
			t.Fatal("pre-fix recording unexpectedly contains max_output_tokens")
		}
		// Keep the pre-fix recording intact; only the expected copy gains the fixed field.
		want["max_output_tokens"] = float64(req.MaxTokens)
	}
	for key := range *got {
		if strings.HasPrefix(key, "_") {
			delete(*got, key)
		}
	}
	for key, value := range want {
		if actual, ok := (*got)[key]; !ok || !reflect.DeepEqual(actual, value) {
			t.Errorf("request field %q differs from recording", key)
		}
	}
	for key := range *got {
		if _, ok := want[key]; !ok {
			t.Errorf("unexpected request field %q", key)
		}
	}
	return r, streamErr
}

func fixtureRequest(t *testing.T, provider, scenario string) Request {
	t.Helper()
	model := map[string]string{"codex": "gpt-6-luna", "openai": "gpt-6-luna", "xai": "grok-build-0.1"}[provider]
	if model == "" {
		t.Fatalf("unknown fixture provider %q", provider)
	}
	r := Request{Model: model, System: "You are a helpful assistant. Answer briefly.", Messages: []Message{Text(User, "Reply with exactly: Hello Talos.")}}
	tool := Tool{Name: "read_file", Description: "Read a text file.", Schema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)}
	switch scenario {
	case "text":
	case "thinking", "thinking-default", "thinking-low", "effort-unsupported":
		r.Effort = "high"
		r.Messages = []Message{Text(User, "Find the smallest positive integer that leaves remainder 2 modulo 3, 3 modulo 5, and 2 modulo 7. Check your answer and briefly explain.")}
		if scenario == "thinking-default" {
			r.Effort = ""
		}
		if scenario == "thinking-low" {
			r.Model, r.Effort = "grok-4.7", "low"
		}
		if provider == "xai" && scenario == "thinking" {
			r.Model, r.MaxTokens = "grok-4.7", 1024
		}
	case "tool", "tool-result":
		r.Tools = []Tool{tool}
		r.Messages = []Message{Text(User, "Call read_file exactly once for hello.txt. Do not guess its contents.")}
		if scenario == "tool-result" {
			previous, err := replayFixture(t, provider, "tool", r)
			if err != nil {
				t.Fatal(err)
			}
			results := Message{Role: User}
			for _, call := range previous.Message.ToolCalls() {
				results.Parts = append(results.Parts, Part{ToolResult: &ToolResult{CallID: call.ID, Name: call.Name, Content: "Hello from the fixture file."}})
			}
			r.Messages = append(r.Messages, previous.Message, results)
		}
	case "tools-parallel":
		r.Tools = []Tool{tool}
		r.Messages = []Message{Text(User, "Call read_file twice in parallel in this response: once for alpha.txt and once for beta.txt. Do not answer until both file contents are available.")}
	case "image":
		var b bytes.Buffer
		img := image.NewRGBA(image.Rect(0, 0, 32, 32))
		for y := 0; y < 32; y++ {
			for x := 0; x < 32; x++ {
				img.SetRGBA(x, y, color.RGBA{R: 255, A: 255})
			}
		}
		if err := png.Encode(&b, img); err != nil {
			t.Fatal(err)
		}
		r.Messages = []Message{{Role: User, Parts: []Part{{Text: "What color is this image? Answer in one word."}, {Image: &Image{MediaType: "image/png", Data: b.Bytes()}}}}}
	case "max-tokens", "max-tokens-unsupported":
		r.MaxTokens = 16
		r.Messages = []Message{Text(User, "Write the numbers from 1 to 100, separated by spaces.")}
	case "error-model":
		r.Model = "talos-fixture-model-does-not-exist"
	case "model-unavailable":
		if provider != "openai" {
			t.Fatalf("unexpected unavailable-model fixture for %q", provider)
		}
		r.Model = "gpt-5-nano"
	case "cancel":
		r.Messages = []Message{Text(User, "Write the numbers from 1 to 500, one number per line.")}
	default:
		t.Fatalf("unknown fixture scenario %q", scenario)
	}
	return r
}
