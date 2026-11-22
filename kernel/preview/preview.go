// Package preview provides a built-in headless browser preview for localhost
// and web URLs, capturing screenshots, console errors, and network failures.
package preview

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ConsoleEntry is one console error/warning or uncaught exception from the page.
type ConsoleEntry struct {
	Level  string `json:"level"`
	Text   string `json:"text"`
	Source string `json:"source,omitempty"`
	Line   int    `json:"line,omitempty"`
}

// NetworkEntry is one failed HTTP request or >=400 response.
type NetworkEntry struct {
	URL    string `json:"url"`
	Status int    `json:"status,omitempty"`
	Method string `json:"method,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Snapshot is the latest state of a previewed page.
type Snapshot struct {
	URL        string         `json:"url"`
	Title      string         `json:"title,omitempty"`
	Status     int            `json:"status,omitempty"`
	Screenshot []byte         `json:"screenshot,omitempty"`
	Console    []ConsoleEntry `json:"console,omitempty"`
	Network    []NetworkEntry `json:"network,omitempty"`
	Error      string         `json:"error,omitempty"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// Manager coordinates headless browser previews and stores the latest snapshot.
type Manager struct {
	// BrowserPath optionally overrides headless browser binary discovery.
	BrowserPath string
	// Runner optionally overrides how a URL is captured (used in tests).
	Runner func(ctx context.Context, targetURL string) (Snapshot, error)

	mu     sync.RWMutex
	latest Snapshot
}

// Default is the process-wide preview manager.
var Default = &Manager{}

// IsLocalURL reports whether rawURL targets localhost / loopback.
func IsLocalURL(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

// NormalizeURL ensures a valid http/https URL, defaulting to http://localhost:3000.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "http://localhost:3000", nil
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("preview: invalid url %q", raw)
	}
	return u.String(), nil
}

// Open navigates to rawURL, captures a screenshot and console/network errors,
// and saves the snapshot.
func (m *Manager) Open(ctx context.Context, rawURL string) (Snapshot, error) {
	target, err := NormalizeURL(rawURL)
	if err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	if m.Runner != nil {
		snap, err = m.Runner(ctx, target)
	} else if os.Getenv("STRATUM_DEMO") != "" {
		snap = demoSnapshot(target)
	} else if bin := m.browserBin(); bin != "" {
		snap, err = captureWithCDP(ctx, bin, target)
	} else {
		snap, err = fallbackHTTPPreview(ctx, target)
	}
	if snap.URL == "" {
		snap.URL = target
	}
	if snap.UpdatedAt.IsZero() {
		snap.UpdatedAt = time.Now()
	}
	if err != nil && snap.Error == "" {
		snap.Error = err.Error()
	}
	m.mu.Lock()
	m.latest = snap
	m.mu.Unlock()
	return snap, err
}

// State returns the latest captured preview snapshot.
func (m *Manager) State() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.latest
}

func (m *Manager) browserBin() string {
	if m.BrowserPath != "" {
		return m.BrowserPath
	}
	return findBrowser()
}

func findBrowser() string {
	candidates := []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

func demoSnapshot(target string) Snapshot {
	return Snapshot{
		URL:        target,
		Title:      "Stratum Local Preview (Demo)",
		Status:     200,
		Screenshot: renderPlaceholderPNG(800, 480, color.RGBA{244, 246, 250, 255}, color.RGBA{59, 130, 246, 255}),
		UpdatedAt:  time.Now(),
	}
}

var titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

func fallbackHTTPPreview(ctx context.Context, target string) (Snapshot, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target, nil)
	if err != nil {
		return Snapshot{URL: target, Error: err.Error()}, err
	}
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		return Snapshot{
			URL:     target,
			Error:   err.Error(),
			Network: []NetworkEntry{{URL: target, Method: http.MethodGet, Error: err.Error()}},
		}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	title := target
	if m := titleRe.FindSubmatch(body); len(m) == 2 {
		if t := strings.TrimSpace(string(m[1])); t != "" {
			title = t
		}
	}
	snap := Snapshot{
		URL:        target,
		Title:      title,
		Status:     resp.StatusCode,
		Screenshot: renderPlaceholderPNG(800, 480, color.RGBA{248, 249, 251, 255}, color.RGBA{34, 197, 94, 255}),
		UpdatedAt:  time.Now(),
	}
	if resp.StatusCode >= 400 {
		snap.Network = append(snap.Network, NetworkEntry{
			URL:    target,
			Status: resp.StatusCode,
			Method: http.MethodGet,
			Error:  resp.Status,
		})
	}
	return snap, nil
}

func renderPlaceholderPNG(w, h int, bg, accent color.RGBA) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if y < 36 {
				img.SetRGBA(x, y, color.RGBA{230, 232, 238, 255})
			} else if y == 36 {
				img.SetRGBA(x, y, color.RGBA{205, 208, 216, 255})
			} else if x >= 32 && x < w-32 && y >= 68 && y < 108 {
				img.SetRGBA(x, y, accent)
			} else {
				img.SetRGBA(x, y, bg)
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func captureWithCDP(ctx context.Context, bin, target string) (Snapshot, error) {
	userDir, err := os.MkdirTemp("", "stratum-preview-*")
	if err != nil {
		return Snapshot{}, err
	}
	defer os.RemoveAll(userDir)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Snapshot{}, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cctx, bin,
		"--headless=new",
		"--disable-gpu",
		"--no-first-run",
		"--no-default-browser-check",
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir="+userDir,
		"--window-size=1280,800",
		"about:blank",
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return Snapshot{}, err
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	wsURL, err := waitForCDPWebSocket(cctx, port)
	if err != nil {
		return Snapshot{}, err
	}
	ws, err := dialWS(cctx, wsURL)
	if err != nil {
		return Snapshot{}, err
	}
	defer ws.close()

	var (
		mu      sync.Mutex
		console []ConsoleEntry
		network []NetworkEntry
		loaded  = make(chan struct{})
		once    sync.Once
	)
	ws.onEvent = func(method string, params json.RawMessage) {
		switch method {
		case "Page.loadEventFired":
			once.Do(func() { close(loaded) })
		case "Runtime.consoleAPICalled":
			var ev struct {
				Type string `json:"type"`
				Args []struct {
					Value       any    `json:"value"`
					Description string `json:"description"`
				} `json:"args"`
			}
			if json.Unmarshal(params, &ev) == nil && (ev.Type == "error" || ev.Type == "warning" || ev.Type == "assert") {
				var parts []string
				for _, a := range ev.Args {
					if a.Description != "" {
						parts = append(parts, a.Description)
					} else if a.Value != nil {
						parts = append(parts, fmt.Sprint(a.Value))
					}
				}
				if len(parts) > 0 {
					mu.Lock()
					console = append(console, ConsoleEntry{Level: ev.Type, Text: strings.Join(parts, " ")})
					mu.Unlock()
				}
			}
		case "Runtime.exceptionThrown":
			var ev struct {
				ExceptionDetails struct {
					Text         string `json:"text"`
					URL          string `json:"url"`
					LineNumber   int    `json:"lineNumber"`
					Exception    struct {
						Description string `json:"description"`
					} `json:"exception"`
				} `json:"exceptionDetails"`
			}
			if json.Unmarshal(params, &ev) == nil {
				msg := ev.ExceptionDetails.Exception.Description
				if msg == "" {
					msg = ev.ExceptionDetails.Text
				}
				mu.Lock()
				console = append(console, ConsoleEntry{
					Level:  "error",
					Text:   msg,
					Source: ev.ExceptionDetails.URL,
					Line:   ev.ExceptionDetails.LineNumber + 1,
				})
				mu.Unlock()
			}
		case "Network.responseReceived":
			var ev struct {
				Response struct {
					URL        string `json:"url"`
					Status     int    `json:"status"`
					StatusText string `json:"statusText"`
				} `json:"response"`
			}
			if json.Unmarshal(params, &ev) == nil && ev.Response.Status >= 400 {
				mu.Lock()
				network = append(network, NetworkEntry{
					URL:    ev.Response.URL,
					Status: ev.Response.Status,
					Error:  ev.Response.StatusText,
				})
				mu.Unlock()
			}
		case "Network.loadingFailed":
			var ev struct {
				ErrorText string `json:"errorText"`
			}
			if json.Unmarshal(params, &ev) == nil && ev.ErrorText != "" {
				mu.Lock()
				network = append(network, NetworkEntry{Error: ev.ErrorText})
				mu.Unlock()
			}
		}
	}

	_, _ = ws.call(cctx, "Page.enable", nil)
	_, _ = ws.call(cctx, "Runtime.enable", nil)
	_, _ = ws.call(cctx, "Network.enable", nil)
	if _, err := ws.call(cctx, "Page.navigate", map[string]any{"url": target}); err != nil {
		return Snapshot{URL: target, Error: err.Error()}, err
	}
	select {
	case <-loaded:
	case <-time.After(10 * time.Second):
	case <-cctx.Done():
	}
	// Brief settle for async console errors after load.
	select {
	case <-time.After(300 * time.Millisecond):
	case <-cctx.Done():
	}

	var title string
	if res, err := ws.call(cctx, "Runtime.evaluate", map[string]any{"expression": "document.title", "returnByValue": true}); err == nil {
		var evalRes struct {
			Result struct {
				Value string `json:"value"`
			} `json:"result"`
		}
		if json.Unmarshal(res, &evalRes) == nil {
			title = evalRes.Result.Value
		}
	}
	var shot []byte
	if res, err := ws.call(cctx, "Page.captureScreenshot", map[string]any{"format": "png"}); err == nil {
		var capRes struct {
			Data string `json:"data"`
		}
		if json.Unmarshal(res, &capRes) == nil && capRes.Data != "" {
			shot, _ = base64.StdEncoding.DecodeString(capRes.Data)
		}
	}

	mu.Lock()
	snap := Snapshot{
		URL:        target,
		Title:      title,
		Status:     200,
		Screenshot: shot,
		Console:    console,
		Network:    network,
		UpdatedAt:  time.Now(),
	}
	mu.Unlock()
	return snap, nil
}

func waitForCDPWebSocket(ctx context.Context, port int) (string, error) {
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/json/list", port)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		resp, err := client.Get(endpoint)
		if err == nil {
			var pages []struct {
				Type                 string `json:"type"`
				WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&pages)
			_ = resp.Body.Close()
			for _, p := range pages {
				if p.Type == "page" && p.WebSocketDebuggerURL != "" {
					return p.WebSocketDebuggerURL, nil
				}
			}
		}
		select {
		case <-time.After(80 * time.Millisecond):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

// Minimal RFC6455 client for CDP communication without external dependencies.
type cdpWS struct {
	conn    net.Conn
	br      *bufio.Reader
	wmu     sync.Mutex
	pmu     sync.Mutex
	pending map[int64]chan cdpResp
	nextID  atomic.Int64
	onEvent func(method string, params json.RawMessage)
	closed  atomic.Bool
}

type cdpResp struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func dialWS(ctx context.Context, rawURL string) (*cdpWS, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, err
	}
	keyBytes := make([]byte, 16)
	_, _ = rand.Read(keyBytes)
	key := base64.StdEncoding.EncodeToString(keyBytes)
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		u.RequestURI(), u.Host, key)
	if _, err := io.WriteString(conn, req); err != nil {
		_ = conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	statusLine, err := br.ReadString('\n')
	if err != nil || !strings.Contains(statusLine, "101") {
		_ = conn.Close()
		return nil, fmt.Errorf("cdp websocket handshake failed: %s %v", strings.TrimSpace(statusLine), err)
	}
	expected := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	accept := base64.StdEncoding.EncodeToString(expected[:])
	gotAccept := false
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Sec-WebSocket-Accept") {
			gotAccept = strings.TrimSpace(v) == accept
		}
	}
	if !gotAccept {
		_ = conn.Close()
		return nil, errors.New("cdp websocket invalid accept key")
	}
	ws := &cdpWS{conn: conn, br: br, pending: make(map[int64]chan cdpResp)}
	go ws.readLoop()
	return ws, nil
}

func (w *cdpWS) close() {
	if w.closed.Swap(true) {
		return
	}
	_ = w.conn.Close()
}

func (w *cdpWS) writeText(payload []byte) error {
	var hdr [14]byte
	hdr[0] = 0x81 // FIN + text frame
	n := len(payload)
	idx := 2
	switch {
	case n <= 125:
		hdr[1] = 0x80 | byte(n)
	case n <= 65535:
		hdr[1] = 0x80 | 126
		binary.BigEndian.PutUint16(hdr[2:4], uint16(n))
		idx = 4
	default:
		hdr[1] = 0x80 | 127
		binary.BigEndian.PutUint64(hdr[2:10], uint64(n))
		idx = 10
	}
	var mask [4]byte
	_, _ = rand.Read(mask[:])
	copy(hdr[idx:idx+4], mask[:])
	idx += 4

	masked := make([]byte, n)
	for i := 0; i < n; i++ {
		masked[i] = payload[i] ^ mask[i%4]
	}

	w.wmu.Lock()
	defer w.wmu.Unlock()
	if _, err := w.conn.Write(hdr[:idx]); err != nil {
		return err
	}
	_, err := w.conn.Write(masked)
	return err
}

func (w *cdpWS) readLoop() {
	for !w.closed.Load() {
		b0, err := w.br.ReadByte()
		if err != nil {
			return
		}
		b1, err := w.br.ReadByte()
		if err != nil {
			return
		}
		opcode := b0 & 0x0F
		masked := (b1 & 0x80) != 0
		length := uint64(b1 & 0x7F)
		if length == 126 {
			var ext [2]byte
			if _, err := io.ReadFull(w.br, ext[:]); err != nil {
				return
			}
			length = uint64(binary.BigEndian.Uint16(ext[:]))
		} else if length == 127 {
			var ext [8]byte
			if _, err := io.ReadFull(w.br, ext[:]); err != nil {
				return
			}
			length = binary.BigEndian.Uint64(ext[:])
		}
		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(w.br, mask[:]); err != nil {
				return
			}
		}
		data := make([]byte, length)
		if _, err := io.ReadFull(w.br, data); err != nil {
			return
		}
		if masked {
			for i := range data {
				data[i] ^= mask[i%4]
			}
		}
		if opcode == 8 { // close
			return
		}
		if opcode != 1 && opcode != 0 {
			continue
		}
		var msg struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg.ID != nil {
			w.pmu.Lock()
			ch := w.pending[*msg.ID]
			delete(w.pending, *msg.ID)
			w.pmu.Unlock()
			if ch != nil {
				ch <- cdpResp{Result: msg.Result, Error: msg.Error}
			}
		} else if msg.Method != "" && w.onEvent != nil {
			w.onEvent(msg.Method, msg.Params)
		}
	}
}

func (w *cdpWS) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := w.nextID.Add(1)
	ch := make(chan cdpResp, 1)
	w.pmu.Lock()
	w.pending[id] = ch
	w.pmu.Unlock()

	b, _ := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err := w.writeText(b); err != nil {
		w.pmu.Lock()
		delete(w.pending, id)
		w.pmu.Unlock()
		return nil, err
	}
	select {
	case r := <-ch:
		if r.Error != nil {
			return nil, errors.New(r.Error.Message)
		}
		return r.Result, nil
	case <-ctx.Done():
		w.pmu.Lock()
		delete(w.pending, id)
		w.pmu.Unlock()
		return nil, ctx.Err()
	}
}

// SaveScreenshot writes the latest screenshot PNG to dstPath if non-empty.
func SaveScreenshot(dstPath string, data []byte) error {
	if len(data) == 0 {
		return errors.New("preview: no screenshot available")
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dstPath, data, 0o644)
}
