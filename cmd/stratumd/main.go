// Command stratumd is Stratum without a window: sign in to model providers and
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

	"maps"
	"slices"

	"golang.org/x/term"

	"github.com/shqingda/stratum/llm/auth"
)

const usage = `usage: stratumd <command> [flags]

commands:
  login <provider>   sign in; stratumd login -h lists the providers
  logout <account>   forget an account's credential
  accounts           list signed-in accounts
  models [provider]  list models (* marks the default)
  chat               talk to a model; -h for flags
  rpc                serve the kernel over stdio (JSON lines)
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
			err = errors.New("usage: stratumd logout <account>")
		} else {
			err = store.Delete(args[0])
		}
	case "accounts":
		err = accounts(store)
	case "rpc", "--rpc":
		err = serveRPC(ctx, store, args)
	case "models":
		err = models(ctx, args)
	case "chat":
		err = chat(ctx, store, args)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		err = fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "stratumd:", err)
		os.Exit(1)
	}
}

func login(ctx context.Context, store auth.Store, args []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	with := fs.String("with", "", "sign in with `method` instead of an API key: "+methodIDs())
	device := fs.Bool("device", false, "codex: sign in with a code on another device")
	as := fs.String("as", "", "store as provider/`label`, for a second account")
	region := fs.String("region", "", "use the provider's regional endpoint, e.g. cn")
	base := fs.String("base-url", "", "the endpoint address (required for custom)")
	api := fs.String("api", "chat", "custom: the protocol, chat, anthropic, responses, or gemini")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: stratumd login <provider> [flags]\n\nproviders:")
		for _, p := range auth.Providers {
			fmt.Fprintf(os.Stderr, "  %-30s %s%s\n", p.ID, p.Name, loginsNote(p))
		}
		fmt.Fprintln(os.Stderr, "\nflags:")
		fs.PrintDefaults()
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fs.Usage()
		os.Exit(2)
	}
	id := args[0]
	fs.Parse(args[1:])
	p, ok := auth.Lookup(id)
	if !ok || strings.Contains(id, "/") {
		return fmt.Errorf("unknown provider %q; run stratumd login -h", id)
	}
	account := id
	if *as != "" {
		account = id + "/" + *as
	}
	if *with == "" && p.KeyURL == "" && len(p.Logins) > 0 {
		*with = p.Logins[0] // the provider takes no key
	}
	if *region != "" {
		if *base = p.Regions[*region]; *base == "" {
			return fmt.Errorf("%s has no region %q", p.Name, *region)
		}
	}

	var c auth.Credential
	var err error
	if *with != "" {
		m := auth.Methods[*with]
		if m == nil || !slices.Contains(p.Logins, *with) {
			return fmt.Errorf("%s does not offer sign-in %q", p.Name, *with)
		}
		in := auth.Interaction{
			OpenURL: func(u string) error {
				fmt.Println(m.Label + ": opening the browser. If it does not open, visit:\n\n  " + u + "\n")
				exec.Command("open", u).Start()
				return nil
			},
			DeviceCode: func(code, uri string) {
				fmt.Printf("%s: visit %s and enter the code\n\n  %s\n\nWaiting…\n", m.Label, uri, code)
				exec.Command("open", uri).Start()
			},
			Paste: func(ctx context.Context) (string, error) {
				fmt.Print("The callback port is busy. After signing in, paste the address the browser shows: ")
				return readLine()
			},
		}
		if codex, ok := m.Flow.(*auth.Codex); ok && *device {
			c, err = codex.LoginDevice(ctx, in)
		} else {
			c, err = m.Flow.Login(ctx, in)
		}
	} else {
		if p.ID == "custom" && *base == "" {
			return errors.New("custom needs -base-url, e.g. http://localhost:11434/v1")
		}
		var key string
		if v := os.Getenv(p.KeyEnv); p.KeyEnv != "" && v != "" {
			fmt.Println("Using the key in $" + p.KeyEnv)
			key = v
		} else {
			if p.KeyURL != "" {
				fmt.Println("Create a key at " + p.KeyURL)
			}
			key, err = readSecret(p.Name + " API key: ")
		}
		if err == nil && key == "" && p.ID != "custom" {
			err = errors.New("no key")
		}
		c = auth.Credential{Kind: auth.KindAPIKey, Key: key}
	}
	if err != nil {
		return err
	}
	c.BaseURL = *base
	if p.ID == "custom" {
		c.API = *api
	}
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

func methodIDs() string {
	ids := slices.Sorted(maps.Keys(auth.Methods))
	return strings.Join(ids, ", ")
}

func loginsNote(p auth.Provider) string {
	if len(p.Logins) == 0 {
		return ""
	}
	return "  (-with " + strings.Join(p.Logins, ", ") + ")"
}

func accounts(store auth.Store) error {
	names, err := store.List()
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Println("No accounts. Sign in with: stratumd login <provider>")
	}
	for _, n := range names {
		c, err := store.Get(n)
		if err != nil {
			fmt.Printf("%-20s  (%v)\n", n, err)
			continue
		}
		detail := string(c.Kind)
		if c.Method != "" {
			detail += " (" + c.Method + ")"
		}
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
