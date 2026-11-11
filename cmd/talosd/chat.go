package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shqingda/talos/llm"
	"github.com/shqingda/talos/llm/auth"
)

// chat runs a conversation in the terminal with two read-only tools, a
// stand-in for the kernel until G1.
func chat(ctx context.Context, store auth.Store, args []string) error {
	fs := flag.NewFlagSet("chat", flag.ExitOnError)
	account := fs.String("account", "", "signed-in account to use (default: the only one)")
	model := fs.String("model", "", "model id (default: the provider's default)")
	effort := fs.String("effort", "", "reasoning effort: low, medium or high")
	prompt := fs.String("p", "", "send one prompt, print the answer and exit")
	demo := fs.Bool("demo", false, "use the offline demo model")
	dir := fs.String("dir", ".", "directory the tools may read")
	fs.Parse(args)

	var p llm.Provider
	var provider string
	var plan bool // the account spends a subscription, so dollars mean nothing
	if *demo {
		p, provider = llm.Demo{}, "demo"
	} else {
		if *account == "" {
			names, err := store.List()
			if err != nil {
				return err
			}
			switch len(names) {
			case 0:
				return errors.New("not signed in; run talosd login <provider>, or chat -demo")
			case 1:
				*account = names[0]
			default:
				return fmt.Errorf("pick an account with -account: %s", strings.Join(names, ", "))
			}
		}
		var err error
		if p, err = auth.Open(store, *account); err != nil {
			return err
		}
		provider, _, _ = strings.Cut(*account, "/")
		if cred, err := store.Get(*account); err == nil {
			plan = auth.Subscription(cred)
		}
	}
	loadCatalog(ctx)
	if *model == "" {
		*model = llm.DefaultModel(provider)
		if *model == "" && !*demo {
			return fmt.Errorf("%s has no default model; pass -model (talosd models %s lists them)", provider, provider)
		}
	}
	root, err := os.OpenRoot(*dir)
	if err != nil {
		return err
	}
	defer root.Close()
	abs, _ := filepath.Abs(*dir)

	c := &conversation{
		p:        p,
		root:     root,
		out:      os.Stdout,
		model:    *model,
		provider: provider,
		plan:     plan,
		req: llm.Request{
			Model:     *model,
			Effort:    *effort,
			SessionID: sessionID(),
			System:    "You are Talos, a coding agent. You can read files in " + abs + " with the tools. Answer briefly.",
			Tools:     tools,
		},
	}
	if *prompt != "" {
		return c.turn(ctx, *prompt)
	}
	fmt.Printf("Talos · %s · %s  (Ctrl-D to quit)\n", or(*account, "demo"), *model)
	for {
		fmt.Print("\n› ")
		line, err := readLine()
		if err == io.EOF {
			fmt.Println()
			return nil
		}
		if err != nil {
			return err
		}
		if line == "" {
			continue
		}
		if err := c.turn(ctx, line); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintln(os.Stderr, "\nerror:", err)
		}
	}
}

type conversation struct {
	p        llm.Provider
	root     *os.Root
	out      io.Writer
	model    string
	provider string
	plan     bool
	req      llm.Request
	usage    llm.Usage
}

const maxSteps = 20

// turn sends a user message and runs tool calls until the model answers.
func (c *conversation) turn(ctx context.Context, text string) error {
	start := len(c.req.Messages)
	c.req.Messages = append(c.req.Messages, llm.Text(llm.User, text))
	for range maxSteps {
		thinking := false
		r, err := llm.Collect(c.p.Stream(ctx, c.req), func(e llm.Event) {
			switch e.Type {
			case llm.ThinkingDelta:
				if !thinking {
					fmt.Fprint(c.out, "\x1b[2m")
					thinking = true
				}
				fmt.Fprint(c.out, e.Delta)
			case llm.TextDelta:
				if thinking {
					fmt.Fprint(c.out, "\x1b[0m\n")
					thinking = false
				}
				fmt.Fprint(c.out, e.Delta)
			}
		})
		if thinking {
			fmt.Fprint(c.out, "\x1b[0m")
		}
		if err != nil {
			c.req.Messages = c.req.Messages[:start] // drop the turn so the user can retry
			return err
		}
		c.usage = c.usage.Add(r.Usage)
		c.req.Messages = append(c.req.Messages, r.Message)
		calls := r.Message.ToolCalls()
		if len(calls) == 0 {
			fmt.Fprintf(c.out, "\n\x1b[2m%s\x1b[0m\n", c.summary(r))
			return nil
		}
		results := llm.Message{Role: llm.User}
		for _, call := range calls {
			out, err := runTool(c.root, call)
			mark := "✓"
			if err != nil {
				out, mark = err.Error(), "✗"
			}
			fmt.Fprintf(c.out, "\n\x1b[2m%s %s %s\x1b[0m\n", mark, call.Name, call.Args)
			results.Parts = append(results.Parts, llm.Part{ToolResult: &llm.ToolResult{CallID: call.ID, Name: call.Name, Content: out, IsError: err != nil}})
		}
		c.req.Messages = append(c.req.Messages, results)
	}
	c.req.Messages = c.req.Messages[:start]
	return fmt.Errorf("stopped after %d steps", maxSteps)
}

func (c *conversation) summary(r llm.Reply) string {
	u := r.Usage
	s := fmt.Sprintf("%s · in %d (cached %d) · out %d", r.Stop, u.Input, u.CacheRead, u.Output)
	if m, ok := llm.FindModel(c.provider, c.model); ok && !c.plan {
		s += fmt.Sprintf(" · session ≈$%.4f", m.Cost(c.usage))
	}
	return s
}

var tools = []llm.Tool{
	{
		Name:        "read_file",
		Description: "Read a text file. The path is relative to the working directory.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
	},
	{
		Name:        "list_dir",
		Description: "List a directory. The path is relative to the working directory; \".\" is the directory itself.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
	},
}

const maxRead = 64 << 10

// runTool runs a tool inside root, which keeps paths from escaping it.
func runTool(root *os.Root, call llm.ToolCall) (string, error) {
	var args struct{ Path string }
	if err := json.Unmarshal(call.Args, &args); err != nil {
		return "", fmt.Errorf("bad arguments: %w", err)
	}
	path := filepath.Clean(or(args.Path, "."))
	switch call.Name {
	case "read_file":
		f, err := root.Open(path)
		if err != nil {
			return "", err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, maxRead+1))
		if err != nil {
			return "", err
		}
		if len(b) > maxRead {
			return string(b[:maxRead]) + "\n[truncated]", nil
		}
		return string(b), nil
	case "list_dir":
		f, err := root.Open(path)
		if err != nil {
			return "", err
		}
		defer f.Close()
		entries, err := f.ReadDir(500)
		if err != nil && err != io.EOF {
			return "", err
		}
		var b strings.Builder
		for _, e := range entries {
			b.WriteString(e.Name())
			if e.IsDir() {
				b.WriteByte('/')
			}
			b.WriteByte('\n')
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("no tool %q", call.Name)
}

func sessionID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// loadCatalog refreshes the model list once a day; offline, the cached or
// built-in list serves.
func loadCatalog(ctx context.Context) {
	llm.LoadCatalog(ctx, filepath.Join(auth.Dir, "models", "models.dev.json"), 24*time.Hour, auth.CatalogNames())
}

func models(ctx context.Context, args []string) error {
	loadCatalog(ctx)
	provider := ""
	if len(args) > 0 {
		provider = args[0]
	}
	for _, m := range llm.Models(provider) {
		def := " "
		if m.ID == llm.DefaultModel(m.Provider) {
			def = "*"
		}
		fmt.Printf("%s %-10s %-40s %7dk  $%g/$%g\n", def, m.Provider, m.ID, m.Context/1000, m.Price.Input, m.Price.Output)
	}
	return nil
}
