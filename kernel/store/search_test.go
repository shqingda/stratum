package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shqingda/talos/llm"
)

func searchStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for _, id := range []string{"first", "second"} {
		if err := s.CreateSession(context.Background(), Session{ID: id, Cwd: "/workspace"}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func searchAppend(t *testing.T, s *Store, id, typ string, message llm.Message) Event {
	t.Helper()
	event, err := s.Append(context.Background(), id, typ, message)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func searchHits(t *testing.T, s *Store, query, id string, want int) []SearchHit {
	t.Helper()
	hits, err := s.Search(context.Background(), query, id, 100)
	if err != nil || len(hits) != want {
		t.Fatalf("search %q in %q = %+v, want %d: %v", query, id, hits, want, err)
	}
	return hits
}

func TestSearchTextAndSyntax(t *testing.T) {
	s := searchStore(t)
	var version string
	if err := s.db.QueryRow(`SELECT sqlite_version()`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	t.Logf("SQLite %s supports FTS5 trigram", version)
	english := searchAppend(t, s, "first", MessageEvent, llm.Text(llm.User, "A distinctive English message about astronomy."))
	searchAppend(t, s, "second", MessageEvent, llm.Text(llm.Assistant, "This astronomy reply belongs to another session."))
	searchAppend(t, s, "first", MessageEvent, llm.Text(llm.Assistant, "中文消息可以使用历史分页和全文检索。"))
	hits := searchHits(t, s, "astronomy", "first", 1)
	if hits[0].Seq != english.Seq || hits[0].SessionID != "first" || hits[0].Role != "user" || !hits[0].At.Equal(time.UnixMilli(english.At.UnixMilli())) || !strings.Contains(hits[0].Snippet, "[astronomy]") {
		t.Fatalf("search metadata/snippet: %+v", hits[0])
	}
	searchHits(t, s, "astronomy", "", 2)
	searchHits(t, s, "历史分页", "first", 1)
	searchHits(t, s, "全文检索", "", 1)
	if short := searchHits(t, s, "中文", "", 1); short[0].Snippet != "[中文]消息可以使用历史分页和全文检索。" {
		t.Fatalf("short snippet %q", short[0].Snippet)
	}
	searchHits(t, s, "%", "", 0)
	searchHits(t, s, " \t\n", "", 0)
	searchHits(t, s, "astronomy", "missing", 0)
	for _, query := range []string{"hello-world", "c++", "path/to/file.go", "sqlite:token", `quote"inside`, `"unterminated`} {
		searchAppend(t, s, "second", MessageEvent, llm.Text(llm.User, "Literal query example: "+query+" here."))
		searchHits(t, s, query, "second", 1)
	}
	for _, query := range []string{"*", "\"", "AND", "NEAR(", "{unknown}:needle", "\x00", "foo\x00bar"} {
		searchHits(t, s, query, "first", 0)
	}
	if hits, err := s.Search(context.Background(), "astronomy", "", 1); err != nil || len(hits) != 1 {
		t.Fatalf("limit: %+v %v", hits, err)
	}
}

func TestSearchExcludesToolAndNonTextData(t *testing.T) {
	s := searchStore(t)
	message := llm.Message{Role: llm.Assistant, Parts: []llm.Part{
		{Text: "Visible narrative."},
		{ToolCall: &llm.ToolCall{ID: "call", Name: "toolonlysecret", Args: json.RawMessage(`{"value":"argumentonlysecret"}`)}},
		{ToolResult: &llm.ToolResult{CallID: "call", Content: "resultonlysecret"}},
		{Thinking: &llm.Thinking{Text: "reasoningonlysecret"}},
	}}
	searchAppend(t, s, "first", MessageEvent, message)
	searchAppend(t, s, "first", MessageEvent, llm.Message{Role: llm.User, Parts: message.Parts[1:]})
	searchAppend(t, s, "first", "note", llm.Text(llm.User, "eventonlysecret"))
	searchHits(t, s, "narrative", "", 1)
	for _, query := range []string{"toolonlysecret", "argumentonlysecret", "resultonlysecret", "reasoningonlysecret", "eventonlysecret"} {
		searchHits(t, s, query, "", 0)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM message_search`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("non-text messages indexed: %d %v", count, err)
	}
}

func TestSearchRankAndTime(t *testing.T) {
	s := searchStore(t)
	old := searchAppend(t, s, "first", MessageEvent, llm.Text(llm.User, "needle "+strings.Repeat("other ", 40)))
	newer := searchAppend(t, s, "second", MessageEvent, llm.Text(llm.User, "needle "+strings.Repeat("other ", 40)))
	best := searchAppend(t, s, "first", MessageEvent, llm.Text(llm.User, "needle needle needle"))
	for i, event := range []Event{best, old, newer} {
		if _, err := s.db.Exec(`UPDATE events SET at=? WHERE session_id=? AND seq=?`, i+1, event.SessionID, event.Seq); err != nil {
			t.Fatal(err)
		}
	}
	hits := searchHits(t, s, "needle", "", 3)
	for i, want := range []Event{best, newer, old} {
		if hits[i].SessionID != want.SessionID || hits[i].Seq != want.Seq {
			t.Fatalf("relevance/time order: %+v", hits)
		}
	}
}

func TestSearchAppendDeleteTransactions(t *testing.T) {
	ctx := context.Background()
	s := searchStore(t)
	if _, err := s.db.Exec(`CREATE TRIGGER fail_update BEFORE UPDATE ON sessions BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(ctx, "first", MessageEvent, llm.Text(llm.User, "rollbackappend")); err == nil {
		t.Fatal("injected append failure was ignored")
	}
	searchHits(t, s, "rollbackappend", "", 0)
	if revision, err := s.Revision(ctx, "first"); err != nil || revision != 0 {
		t.Fatalf("failed append left event: %d %v", revision, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(ctx, "first", MessageEvent, map[string]any{"parts": "invalid"}); err == nil {
		t.Fatal("invalid message indexed")
	}
	if revision, err := s.Revision(ctx, "first"); err != nil || revision != 0 {
		t.Fatalf("invalid message left event: %d %v", revision, err)
	}
	searchAppend(t, s, "first", MessageEvent, llm.Text(llm.User, "deletionmarker"))
	searchAppend(t, s, "second", MessageEvent, llm.Text(llm.User, "deletionmarker"))
	if _, err := s.db.Exec(`CREATE TRIGGER fail_delete BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSession(ctx, "first"); err == nil {
		t.Fatal("injected deletion failure was ignored")
	}
	searchHits(t, s, "deletionmarker", "first", 1)
	if _, err := s.db.Exec(`DROP TRIGGER fail_delete`); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSession(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	searchHits(t, s, "deletionmarker", "first", 0)
	searchHits(t, s, "deletionmarker", "", 1)
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM message_search WHERE session_id='first'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleted session left index entries: %d %v", count, err)
	}
}

func legacySearchDB(t *testing.T, malformed bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(schema + `PRAGMA user_version=1; INSERT INTO sessions(id,cwd,created_at,updated_at) VALUES ('legacy','/w',1,1);`); err != nil {
		t.Fatal(err)
	}
	message, err := json.Marshal(llm.Text(llm.User, "migratedmessage 中文历史分页"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO events(session_id,seq,type,data,at) VALUES ('legacy',1,'message',?,1)`, message); err != nil {
		t.Fatal(err)
	}
	data := `{"role":"assistant","parts":[{"toolResult":{"callId":"c","content":"migrationtoolsecret"}}]}`
	if malformed {
		data = `{"parts":42}`
	}
	if _, err := db.Exec(`INSERT INTO events(session_id,seq,type,data,at) VALUES ('legacy',2,'message',?,2)`, []byte(data)); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSearchMigration(t *testing.T) {
	path := legacySearchDB(t, false)
	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		searchHits(t, s, "migratedmessage", "", 1)
		searchHits(t, s, "历史分页", "", 1)
		searchHits(t, s, "migrationtoolsecret", "", 0)
		var version, count int
		if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 2 {
			t.Fatalf("schema version: %d %v", version, err)
		}
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM message_search`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("backfill repeated: %d %v", count, err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`PRAGMA user_version=3`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 3 {
		t.Fatalf("open downgraded version: %d %v", version, err)
	}
}

func TestSearchMigrationRollback(t *testing.T) {
	path := legacySearchDB(t, true)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WaitSearchReady(context.Background()); err == nil {
		s.Close()
		t.Fatal("malformed legacy message silently ignored")
	}
	s.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, tables, events int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 1 {
		t.Fatalf("failed migration changed version: %d %v", version, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='message_search'`).Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("failed migration left partial index: %d %v", tables, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&events); err != nil || events != 2 {
		t.Fatalf("migration lost source events: %d %v", events, err)
	}
}

func TestOpenTurnSessions(t *testing.T) {
	s := searchStore(t)
	ctx := context.Background()
	if _, err := s.Append(ctx, "first", "turn.start", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(ctx, "second", "turn.start", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(ctx, "second", "turn.end", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	ids, err := s.OpenTurnSessions(ctx, "turn.start", "turn.end")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "first" {
		t.Fatalf("OpenTurnSessions = %v, want [first]", ids)
	}
}
