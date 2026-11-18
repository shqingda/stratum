package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/shqingda/talos/kernel/store"
	"github.com/shqingda/talos/llm"
	"github.com/shqingda/talos/proto"
)

func TestNextCronRun(t *testing.T) {
	base := time.Date(2026, 11, 18, 8, 15, 0, 0, time.UTC)
	next, err := NextCronRun("0 9 * * 1-5", base)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC) // 2026-11-18 is Wednesday
	if !next.Equal(want) {
		t.Fatalf("got %v, want %v", next, want)
	}

	nextH, err := NextCronRun("@hourly", base)
	if err != nil {
		t.Fatal(err)
	}
	if !nextH.Equal(want) {
		t.Fatalf("@hourly got %v, want %v", nextH, want)
	}

	nextE, err := NextCronRun("@every 30m", base)
	if err != nil {
		t.Fatal(err)
	}
	if !nextE.Equal(base.Add(30 * time.Minute)) {
		t.Fatalf("@every 30m got %v", nextE)
	}

	if _, err := NextCronRun("bad cron", base); err == nil {
		t.Fatal("expected error for invalid cron")
	}
}

func TestAutomationsCRUDAndRunNow(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := New(st, func(string) (llm.Provider, error) { return &llm.Script{}, nil })
	m.Defaults = func() (string, string) { return "script", "script" }
	defer m.Close()

	cwd := t.TempDir()
	created, err := m.UpsertAutomation(ctx, proto.Automation{
		Name:     "Nightly Check",
		Prompt:   "Run go vet ./...",
		Cwd:      cwd,
		Schedule: "0 9 * * *",
		Enabled:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.NextRunAt.IsZero() {
		t.Fatalf("expected ID and NextRunAt, got %+v", created)
	}

	list, err := m.ListAutomations(ctx)
	if err != nil || len(list.Items) != 1 {
		t.Fatalf("ListAutomations: %+v %v", list, err)
	}

	ran, err := m.RunAutomationNow(ctx, created.ID)
	if err != nil {
		t.Fatalf("RunAutomationNow: %v", err)
	}
	if ran.LastSessionID == "" || ran.LastStatus != "ok" {
		t.Fatalf("expected session and ok status, got %+v", ran)
	}

	if err := m.DeleteAutomation(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	list2, err := m.ListAutomations(ctx)
	if err != nil || len(list2.Items) != 0 {
		t.Fatalf("expected empty list after delete, got %+v %v", list2, err)
	}
}
