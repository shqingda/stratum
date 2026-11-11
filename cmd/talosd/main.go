// Command talosd is Talos without a window: sign in to model providers and
// chat with a model from the terminal. Later milestones add --rpc, the
// kernel served over stdio.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"

	"golang.org/x/term"

	"talos/llm/auth"
)

const usage = `usage: talosd <command> [flags]

commands:
  login <provider>   sign in (chatgpt, anthropic, openai, google, openrouter, deepseek, custom)
  logout <account>   forget an account's credential
  accounts           list signed-in accounts
  chat               talk to a model; -h for flags
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	store := &auth.Keychain{}
	var err error
	switch cmd, args := os.Args[1], os.Args[2:]; cmd {
	case "login":
		err = login(ctx, store, args)
	case "logout":
		if len(args) != 1 {
			err = errors.New("usage: talosd logout <account>")
		} else {
			err = store.Delete(args[0])
		}
	case "accounts":
		err = accounts(store)
	case "chat":
		err = chat(ctx, store, args)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		err = fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "talosd:", err)
		os.Exit(1)
	}
}

func login(ctx context.Context, store auth.Store, args []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	device := fs.Bool("device", false, "chatgpt: sign in with a code on another device")
	as := fs.String("as", "", "store as provider/`label`, for a second account")
	base := fs.String("base-url", "", "the endpoint address (required for custom)")
	fs.Usage = func() { fmt.Fprintln(os.Stderr, "usage: talosd login <provider> [flags]"); fs.PrintDefaults() }
	if len(args) == 0 {
		fs.Usage()
		os.Exit(2)
	}
	id := args[0]
	fs.Parse(args[1:])
	p, ok := auth.Lookup(id)
	if !ok || strings.Contains(id, "/") {
		return fmt.Errorf("unknown provider %q", id)
	}
	account := id
	if *as != "" {
		account = id + "/" + *as
	}

	var c auth.Credential
	var err error
	if p.OAuth {
		codex := &auth.Codex{}
		in := auth.Interaction{
			OpenURL: func(u string) error {
				fmt.Println("Opening the browser to sign in. If it does not open, visit:\n\n  " + u + "\n")
				exec.Command("open", u).Start()
				return nil
			},
			DeviceCode: func(code, uri string) {
				fmt.Printf("Visit %s and enter the code:\n\n  %s\n\nWaiting…\n", uri, code)
			},
			Paste: func(ctx context.Context) (string, error) {
				fmt.Print("Port 1455 is busy. After signing in, paste the address the browser shows: ")
				return readLine()
			},
		}
		if *device {
			c, err = codex.LoginDevice(ctx, in)
		} else {
			c, err = codex.Login(ctx, in)
		}
	} else {
		if p.ID == "custom" && *base == "" {
			return errors.New("custom needs -base-url, e.g. http://localhost:11434/v1")
		}
		if p.KeyURL != "" {
			fmt.Println("Create a key at " + p.KeyURL)
		}
		var key string
		key, err = readSecret(p.Name + " API key: ")
		if err == nil && key == "" && p.ID != "custom" {
			err = errors.New("no key")
		}
		c = auth.Credential{Kind: auth.KindAPIKey, Key: key}
	}
	if err != nil {
		return err
	}
	c.BaseURL = *base
	if err := store.Set(account, c); err != nil {
		return err
	}
	who := account
	if c.Email != "" {
		who += " (" + c.Email + ")"
	}
	fmt.Println("Signed in:", who)
	return nil
}

func accounts(store auth.Store) error {
	names, err := store.List()
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Println("No accounts. Sign in with: talosd login <provider>")
	}
	for _, n := range names {
		c, err := store.Get(n)
		if err != nil {
			fmt.Printf("%-20s  (%v)\n", n, err)
			continue
		}
		detail := string(c.Kind)
		if c.Email != "" {
			detail += "  " + c.Email
		}
		if !c.Expires.IsZero() {
			detail += "  token until " + c.Expires.Local().Format("Jan 2 15:04")
		}
		fmt.Printf("%-20s  %s\n", n, detail)
	}
	return nil
}

var stdin = bufio.NewReader(os.Stdin)

func readLine() (string, error) {
	s, err := stdin.ReadString('\n')
	if err != nil && s == "" {
		return "", err
	}
	return strings.TrimSpace(s), nil
}

func readSecret(prompt string) (string, error) {
	fmt.Print(prompt)
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return readLine()
	}
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return strings.TrimSpace(string(b)), err
}
