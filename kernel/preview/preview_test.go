package preview

import (
	"bytes"
	"context"
	"image/png"
	"testing"
	"time"
)

func TestPreviewManagerAndDemo(t *testing.T) {
	t.Setenv("STRATUM_DEMO", "1")
	m := &Manager{}
	snap, err := m.Open(context.Background(), "localhost:5173")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if snap.URL != "http://localhost:5173" {
		t.Fatalf("expected normalized URL, got %q", snap.URL)
	}
	if len(snap.Screenshot) == 0 {
		t.Fatalf("expected non-empty screenshot")
	}
	if _, err := png.Decode(bytes.NewReader(snap.Screenshot)); err != nil {
		t.Fatalf("invalid PNG screenshot: %v", err)
	}
	got := m.State()
	if got.URL != snap.URL || got.Title != snap.Title {
		t.Fatalf("State mismatch: %+v vs %+v", got, snap)
	}
}

func TestPreviewCustomRunnerAndConsoleErrors(t *testing.T) {
	m := &Manager{
		Runner: func(ctx context.Context, targetURL string) (Snapshot, error) {
			return Snapshot{
				URL:       targetURL,
				Title:     "Test App",
				Status:    200,
				Console:   []ConsoleEntry{{Level: "error", Text: "Uncaught TypeError: x is undefined", Source: "app.js", Line: 12}},
				Network:   []NetworkEntry{{URL: targetURL + "/api/data", Status: 500, Error: "Internal Server Error"}},
				UpdatedAt: time.Now(),
			}, nil
		},
	}
	snap, err := m.Open(context.Background(), "http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(snap.Console) != 1 || len(snap.Network) != 1 {
		t.Fatalf("expected 1 console & 1 network error, got %+v", snap)
	}
}
