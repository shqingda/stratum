// Package sandbox runs shell commands under macOS Seatbelt: no network,
// writes only to the workspace, a private temporary directory and build
// caches, the workspace's .git read-only, and the user's secret stores
// unreadable.
package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Available tells whether commands can run sandboxed here.
func Available() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	_, err := os.Stat("/usr/bin/sandbox-exec")
	return err == nil
}

// profile is the Seatbelt policy. Parameters: ROOT (workspace), TMP
// (private temporary directory), HOME.
const profile = `(version 1)
(deny default)

(allow process-exec process-fork)
(allow signal (target same-sandbox))
(allow sysctl-read)
(allow mach-lookup)
(allow ipc-posix-sem ipc-posix-shm)
(allow iokit-open)
(allow file-ioctl)
(allow pseudo-tty)

; Reading: everything but the user's secrets.
(allow file-read*)
(deny file-read*
  (subpath (string-append (param "HOME") "/.ssh"))
  (subpath (string-append (param "HOME") "/.aws"))
  (subpath (string-append (param "HOME") "/.gnupg"))
  (subpath (string-append (param "HOME") "/.talos"))
  (subpath (string-append (param "HOME") "/.config/gh"))
  (subpath (string-append (param "HOME") "/.docker"))
  (subpath (string-append (param "HOME") "/Library/Keychains"))
  (literal (string-append (param "HOME") "/.netrc")))

; Writing: the workspace but its .git, the temporary directory, build
; caches and devices.
(allow file-write*
  (subpath (param "ROOT"))
  (subpath (param "TMP"))
  (subpath (string-append (param "HOME") "/Library/Caches"))
  (subpath (string-append (param "HOME") "/.cache"))
  (subpath (string-append (param "HOME") "/go/pkg/mod/cache"))
  (literal "/dev/null") (literal "/dev/zero") (literal "/dev/tty")
  (regex #"^/dev/ttys[0-9]+$"))
(deny file-write* (subpath (string-append (param "ROOT") "/.git")))

; No network, not even local ports.
(deny network*)
`

// Command makes a command that runs line in zsh inside the sandbox, with
// root as its working directory. The caller removes tmp when done.
func Command(ctx context.Context, root, line string) (cmd *exec.Cmd, tmp string, err error) {
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, "", err
	}
	root, _ = filepath.Abs(root)
	tmp, err = os.MkdirTemp("", "talos-exec-")
	if err != nil {
		return nil, "", err
	}
	tmp, _ = filepath.EvalSymlinks(tmp)
	home, _ := os.UserHomeDir()
	if h, err := filepath.EvalSymlinks(home); err == nil {
		home = h
	}
	cmd = exec.CommandContext(ctx, "/usr/bin/sandbox-exec",
		"-p", profile,
		"-D", "ROOT="+root, "-D", "TMP="+tmp, "-D", "HOME="+home,
		"/bin/zsh", "-c", line)
	cmd.Dir = root
	cmd.Env = append(env(), "TMPDIR="+tmp+"/")
	return cmd, tmp, nil
}

// Unsandboxed makes a command that runs line in zsh with no sandbox, for
// calls the user approved as unrestricted.
func Unsandboxed(ctx context.Context, root, line string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "/bin/zsh", "-c", line)
	cmd.Dir = root
	cmd.Env = env()
	return cmd
}

// env is the environment of a command: the user's, without credentials
// that could leak through output, and with no pager or prompts.
func env() []string {
	var out []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if secretVar(k) {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "PAGER=cat", "GIT_PAGER=cat", "GIT_TERMINAL_PROMPT=0", "NO_COLOR=1", "TERM=dumb")
}

func secretVar(k string) bool {
	u := strings.ToUpper(k)
	for _, s := range []string{"KEY", "TOKEN", "SECRET", "PASSWORD", "CREDENTIAL"} {
		if strings.Contains(u, s) {
			return true
		}
	}
	return false
}

// Describe says, for the model, what a sandboxed command can do.
func Describe() string {
	return fmt.Sprint("Commands run in zsh inside a sandbox: no network; writes only inside the workspace (not .git) and $TMPDIR.")
}
