package sandbox

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func escapeRun(t *testing.T, root, line string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd, tmp, err := Command(ctx, root, line)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("sandbox command timed out, not a verified denial: %s", out)
	}
	return string(out), err
}

func escapeQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func escapeWrite(t *testing.T, path, text string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
}

func escapeSetup(t *testing.T) (root, outside, home string) {
	t.Helper()
	if !Available() {
		t.Skip("no sandbox-exec")
	}
	base := t.TempDir()
	root, outside, home = filepath.Join(base, "workspace"), filepath.Join(base, "other-user"), filepath.Join(base, "home")
	for _, dir := range []string{root, outside, home} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	return root, outside, home
}

func TestSandboxEscapePaths(t *testing.T) {
	root, outside, home := escapeSetup(t)
	victim := filepath.Join(outside, "victim")
	const original = "outside unchanged\n"
	escapeWrite(t, victim, original, 0o600)
	escapeWrite(t, filepath.Join(root, "inside"), "inside unchanged\n", 0o600)
	for link, target := range map[string]string{
		"file-link": victim, "chain-first": "chain-second", "chain-second": "file-link",
		"dir-link": outside, "nested-dir-link": "dir-link",
	} {
		if err := os.Symlink(target, filepath.Join(root, link)); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := escapeRun(t, root, `printf allowed > allowed && printf allowed > "$TMPDIR/allowed"`); err != nil {
		t.Fatalf("sandbox cannot perform allowed writes: %v %s", err, out)
	}
	cases := []struct{ name, command, target string }{
		{"file-symlink", "printf escaped > file-link", victim},
		{"symlink-chain", "printf escaped > chain-first", victim},
		{"directory-chain", "printf escaped > nested-dir-link/victim", victim},
		{"hardlink-into-workspace", "ln " + escapeQuote(victim) + " hardlink-in && printf escaped > hardlink-in", victim},
		{"hardlink-outside", "ln inside " + escapeQuote(filepath.Join(outside, "hardlink-out")), filepath.Join(outside, "hardlink-out")},
		{"dotdot", "printf escaped > ../other-user/victim", victim},
		{"absolute-home", `printf escaped > "$HOME/marker"`, filepath.Join(home, "marker")},
		{"absolute-other-user", "printf escaped > " + escapeQuote(victim), victim},
		{"fork-inherits-policy", "/bin/sh -c " + escapeQuote("/bin/sh -c "+escapeQuote("printf escaped > "+escapeQuote(victim))), victim},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := escapeRun(t, root, tc.command)
			if err == nil {
				t.Errorf("escape command succeeded: %s", out)
			}
			got, readErr := os.ReadFile(tc.target)
			if tc.target == victim {
				if readErr != nil || string(got) != original {
					t.Errorf("outside file changed: %q, %v", got, readErr)
				}
			} else if !os.IsNotExist(readErr) {
				t.Errorf("outside target created or inaccessible: %v", readErr)
			}
		})
	}
}

func TestSandboxEscapeGit(t *testing.T) {
	root, _, _ := escapeSetup(t)
	for _, args := range [][]string{
		{"init", "-q", root}, {"-C", root, "config", "user.name", "Sandbox Test"},
		{"-C", root, "config", "user.email", "sandbox@example.invalid"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("prepare repository: %v %s", err, out)
		}
	}
	escapeWrite(t, filepath.Join(root, "tracked"), "test\n", 0o600)
	if out, err := exec.Command("git", "-C", root, "add", "tracked").CombinedOutput(); err != nil {
		t.Fatalf("prepare index: %v %s", err, out)
	}
	config, err := os.ReadFile(filepath.Join(root, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	for name, line := range map[string]string{
		"new-file":        "printf escaped > .git/marker",
		"object-file":     "printf escaped > .git/objects/marker",
		"existing-config": "printf escaped > .git/config",
		"git-config":      "git config --local escape.test true",
		"git-commit":      "git -c core.hooksPath=/dev/null commit -m sandbox-test",
	} {
		t.Run(name, func(t *testing.T) {
			if out, err := escapeRun(t, root, line); err == nil {
				t.Errorf("git write succeeded: %s", out)
			}
			if got, err := os.ReadFile(filepath.Join(root, ".git", "config")); err != nil || string(got) != string(config) {
				t.Errorf("git config changed: %v", err)
			}
			for _, path := range []string{"marker", "objects/marker", "refs/heads/main", "refs/heads/master"} {
				if _, err := os.Stat(filepath.Join(root, ".git", path)); !os.IsNotExist(err) {
					t.Errorf("git state %s created or inaccessible: %v", path, err)
				}
			}
		})
	}
}

func TestSandboxEscapeNetwork(t *testing.T) {
	root, _, _ := escapeSetup(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var connections atomic.Int32
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
			fmt.Fprint(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
			conn.Close()
		}
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	for name, line := range map[string]string{
		"curl": fmt.Sprintf("/usr/bin/curl --noproxy '*' -fsS --max-time 2 http://127.0.0.1:%d/", port),
		"nc":   fmt.Sprintf("/usr/bin/nc -z -w 2 127.0.0.1 %d", port),
	} {
		t.Run(name, func(t *testing.T) {
			before := connections.Load()
			if out, err := exec.Command("/bin/zsh", "-c", line).CombinedOutput(); err != nil {
				t.Fatalf("local TCP control failed: %v %s", err, out)
			}
			deadline := time.Now().Add(time.Second)
			for connections.Load() != before+1 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if connections.Load() != before+1 {
				t.Fatal("local TCP control did not reach listener")
			}
			if out, err := escapeRun(t, root, line); err == nil {
				t.Errorf("network command succeeded: %s", out)
			}
			if connections.Load() != before+1 {
				t.Errorf("sandbox reached TCP listener %d times", connections.Load())
			}
		})
	}

	t.Run("dns", func(t *testing.T) {
		dns, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer dns.Close()
		var queries atomic.Int32
		go func() {
			buf := make([]byte, 4096)
			for {
				n, addr, err := dns.ReadFrom(buf)
				if err != nil {
					return
				}
				queries.Add(1)
				if n >= 12 {
					buf[2], buf[3] = 0x81, 0x83 // Local NXDOMAIN; never forward to a public resolver.
					dns.WriteTo(buf[:n], addr)
				}
			}
		}()
		args := []string{"+tries=1", "+time=1", "+noedns", "@127.0.0.1", "-p", fmt.Sprint(dns.LocalAddr().(*net.UDPAddr).Port), "talos-escape.example.invalid", "A"}
		if out, err := exec.Command("/usr/bin/dig", args...).CombinedOutput(); err != nil || queries.Load() != 1 {
			t.Fatalf("local DNS control failed: %v %s", err, out)
		}
		if out, err := escapeRun(t, root, "/usr/bin/dig "+strings.Join(args, " ")); err == nil {
			t.Errorf("sandbox DNS command succeeded: %s", out)
		}
		if queries.Load() != 1 {
			t.Errorf("sandbox sent a DNS query to the external-domain resolver")
		}
	})
}

func TestSandboxEscapeEnvironment(t *testing.T) {
	root, _, _ := escapeSetup(t)
	keys := []string{"OPENAI_API_KEY", "GITHUB_TOKEN", "AWS_SECRET_ACCESS_KEY", "TALOS_ESCAPE_TOKEN"}
	for _, key := range keys {
		t.Setenv(key, "synthetic-escape-secret")
	}
	t.Setenv("TALOS_ESCAPE_VISIBLE", "visible-control")
	out, err := escapeRun(t, root, "/usr/bin/env")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "TALOS_ESCAPE_VISIBLE=visible-control\n") {
		t.Fatal("env did not run normally")
	}
	for _, key := range keys {
		if strings.Contains(out, key+"=") || strings.Contains(out, "synthetic-escape-secret") {
			t.Errorf("credential variable %s leaked", key)
		}
	}
}

func TestSandboxEscapeLaunch(t *testing.T) {
	root, outside, _ := escapeSetup(t)
	t.Run("osascript", func(t *testing.T) {
		marker := filepath.Join(outside, "osascript-marker")
		script := "do shell script " + fmt.Sprintf("%q", "printf escaped > "+escapeQuote(marker))
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "/usr/bin/osascript", "-e", script).CombinedOutput(); err != nil {
			t.Fatalf("harmless osascript control failed: %v %s", err, out)
		}
		if got, err := os.ReadFile(marker); err != nil || string(got) != "escaped" {
			t.Fatalf("osascript control did not run marker: %q %v", got, err)
		}
		if err := os.Remove(marker); err != nil {
			t.Fatal(err)
		}
		if out, err := escapeRun(t, root, "/usr/bin/osascript -e "+escapeQuote(script)); err == nil {
			t.Errorf("osascript escaped: %s", out)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Errorf("osascript wrote outside: %v", err)
		}
	})
	t.Run("open", func(t *testing.T) {
		app := filepath.Join(root, "EscapeProbe.app")
		bin := filepath.Join(app, "Contents", "MacOS")
		if err := os.MkdirAll(bin, 0o700); err != nil {
			t.Fatal(err)
		}
		marker := filepath.Join(outside, "open-marker")
		escapeWrite(t, filepath.Join(bin, "probe"), "#!/bin/sh\nprintf escaped > "+escapeQuote(marker)+"\n", 0o700)
		escapeWrite(t, filepath.Join(app, "Contents", "Info.plist"), `<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleExecutable</key><string>probe</string><key>CFBundleIdentifier</key><string>invalid.example.talos.escapeprobe</string><key>CFBundlePackageType</key><string>APPL</string><key>LSBackgroundOnly</key><true/></dict></plist>`, 0o600)
		// A successful control proves the app is runnable without touching a real app or document.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "/usr/bin/open", "-W", "-n", "-g", app).CombinedOutput(); err != nil {
			t.Fatalf("harmless launch control failed: %v %s", err, out)
		}
		if got, err := os.ReadFile(marker); err != nil || string(got) != "escaped" {
			t.Fatalf("launch control did not run marker: %q %v", got, err)
		}
		if err := os.Remove(marker); err != nil {
			t.Fatal(err)
		}
		if out, err := escapeRun(t, root, "/usr/bin/open -W -n -g "+escapeQuote(app)); err == nil {
			t.Errorf("open launched outside sandbox: %s", out)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Errorf("open wrote outside: %v", err)
		}
	})
}
