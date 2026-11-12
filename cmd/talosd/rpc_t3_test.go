package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/shqingda/talos/llm"
	"github.com/shqingda/talos/proto"
)

func TestRPCChangesRevertRedo(t *testing.T) {
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	created, edited := filepath.Join(repo, "created.txt"), filepath.Join(repo, "edited.txt")
	before, after, content, manual := "first\nsecond\n", "first\nupdated\n", "created\n", "manual\n"
	if err := os.WriteFile(edited, []byte(before), 0600); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(t.TempDir(), "talos.db")
	k := start(t, db, []llm.Turn{
		{Calls: []llm.ToolCall{
			call("create", "write_file", map[string]string{"path": "created.txt", "content": content}),
			call("edit", "edit_file", map[string]string{"path": "edited.txt", "old_text": "second", "new_text": "updated"}),
		}},
		{Text: "Both files changed."},
	})
	var s proto.Session
	k.call(proto.MethodSessionCreate, proto.SessionCreate{Cwd: repo, Mode: "auto"}, &s)
	k.call(proto.MethodPrompt, proto.Prompt{SessionID: s.ID, Text: "Create one file and edit the other."}, nil)
	turnSeq := awaitChangesTurn(t, k, s.ID)
	query := proto.ChangeQuery{SessionID: s.ID, TurnSeq: turnSeq}

	checkFile := func(path, want string) {
		t.Helper()
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, want %q (error %v)", path, got, want, err)
		}
	}
	checkHistory := func(k *kernel) {
		t.Helper()
		var turns []proto.ChangeTurn
		k.call("changes.list", proto.SessionRef{SessionID: s.ID}, &turns)
		if len(turns) != 1 || turns[0].TurnSeq != turnSeq || len(turns[0].Files) != 2 {
			t.Fatalf("changes.list = %+v, want one turn %d with two files", turns, turnSeq)
		}
		want := map[string]proto.ChangeFile{
			created: {Path: created, Added: 1},
			edited:  {Path: edited, Added: 1, Removed: 1},
		}
		for _, file := range turns[0].Files {
			if expected, ok := want[file.Path]; !ok || !reflect.DeepEqual(file, expected) {
				t.Errorf("listed file = %+v, want %+v", file, expected)
			}
			delete(want, file.Path)
		}
		if len(want) != 0 {
			t.Errorf("files missing from changes.list: %+v", want)
		}
		for _, expected := range []proto.ChangeDiff{
			{Path: created, After: &content},
			{Path: edited, Before: &before, After: &after},
		} {
			q := query
			q.Path = expected.Path
			var diff proto.ChangeDiff
			k.call("changes.diff", q, &diff)
			if !reflect.DeepEqual(diff, expected) {
				t.Errorf("changes.diff for %s = %+v, want %+v", expected.Path, diff, expected)
			}
		}
	}
	checkFile(created, content)
	checkFile(edited, after)
	checkHistory(k)

	if err := os.WriteFile(edited, []byte(manual), 0600); err != nil {
		t.Fatal(err)
	}
	var reverted proto.ChangeResult
	k.call("changes.revert", query, &reverted)
	if reverted.TurnSeq != turnSeq || !reflect.DeepEqual(reverted.Paths, []string{created}) || !reflect.DeepEqual(reverted.Conflicts, []string{edited}) {
		t.Fatalf("revert = %+v, want created restored and edited conflicted", reverted)
	}
	if _, err := os.Stat(created); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reverted new file still exists: %v", err)
	}
	checkFile(edited, manual)
	revertEvent := k.await("changes.reverted", nil)
	checkChangeEvent(t, revertEvent, s.ID, turnSeq, reverted)

	if err := k.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	k.cmd.Wait()
	k2 := start(t, db, []llm.Turn{{Text: "No model call is needed to review changes."}})
	if k2.ready.Instance == k.ready.Instance {
		t.Fatal("kernel instance did not change on restart")
	}
	checkHistory(k2)
	checkLoggedChange(t, k2, revertEvent, turnSeq, reverted)

	var redone proto.ChangeResult
	k2.call("changes.redo", query, &redone)
	if redone.TurnSeq != turnSeq || !reflect.DeepEqual(redone.Paths, []string{created}) || !reflect.DeepEqual(redone.Conflicts, []string{edited}) {
		t.Fatalf("redo = %+v, want created restored and edited conflicted", redone)
	}
	checkFile(created, content)
	checkFile(edited, manual)
	redoEvent := k2.await("changes.redone", nil)
	checkChangeEvent(t, redoEvent, s.ID, turnSeq, redone)
	if redoEvent.Seq <= revertEvent.Seq {
		t.Fatalf("redo event seq %d is not after revert seq %d", redoEvent.Seq, revertEvent.Seq)
	}
	checkLoggedChange(t, k2, redoEvent, turnSeq, redone)
	checkHistory(k2)
}

func awaitChangesTurn(t *testing.T, k *kernel, sessionID string) int64 {
	t.Helper()
	var seen []proto.Event
	end := k.await(proto.EventTurnEnd, &seen)
	var result proto.TurnEnd
	if err := json.Unmarshal(end.Data, &result); err != nil {
		t.Fatal(err)
	}
	if end.SessionID != sessionID || result.Error != "" || result.Stopped {
		t.Fatalf("unexpected turn end: %+v, %+v", end, result)
	}
	var turnSeq int64
	for _, event := range seen {
		if event.Type == proto.EventTurnStart && event.SessionID == sessionID {
			turnSeq = event.Seq
		}
	}
	if turnSeq <= 0 {
		t.Fatal("turn start has no durable sequence")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var snap proto.Snapshot
		k.call(proto.MethodSessionGet, proto.SessionRef{SessionID: sessionID}, &snap)
		if !snap.Running {
			return turnSeq
		}
		if time.Now().After(deadline) {
			t.Fatal("session is still running after turn.end")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func checkChangeEvent(t *testing.T, event proto.Event, sessionID string, turnSeq int64, result proto.ChangeResult) {
	t.Helper()
	if event.SessionID != sessionID || event.Seq <= 0 {
		t.Fatalf("change event is not durable for the session: %+v", event)
	}
	var data struct {
		TurnSeq   int64    `json:"turnSeq"`
		Paths     []string `json:"paths"`
		Conflicts []string `json:"conflicts"`
	}
	if err := json.Unmarshal(event.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.TurnSeq != turnSeq || !reflect.DeepEqual(data.Paths, result.Paths) || !reflect.DeepEqual(data.Conflicts, result.Conflicts) {
		t.Errorf("change event = %+v, want turn %d, result %+v", data, turnSeq, result)
	}
}

func checkLoggedChange(t *testing.T, k *kernel, live proto.Event, turnSeq int64, result proto.ChangeResult) {
	t.Helper()
	var page proto.EventPage
	k.call(proto.MethodEvents, proto.EventsQuery{SessionID: live.SessionID, After: live.Seq - 1}, &page)
	for _, event := range page.Events {
		if event.Seq == live.Seq {
			if event.Type != live.Type {
				t.Fatalf("logged change type = %q, want %q", event.Type, live.Type)
			}
			checkChangeEvent(t, event, live.SessionID, turnSeq, result)
			return
		}
	}
	t.Fatalf("change event %s seq %d is missing from durable history", live.Type, live.Seq)
}
