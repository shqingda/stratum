package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestRecordTransport(t *testing.T) {
	const body = "event: test\r\ndata: {\"text\":\"hello\"}\r\n\r\n"
	const request = `{"model":"test"}`
	const secret = "header-must-never-be-saved"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, err := io.ReadAll(r.Body)
		if err != nil || string(got) != request || r.Header.Get("Authorization") != secret {
			t.Errorf("request changed: %q, %v", got, err)
		}
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, body)
	}))
	defer srv.Close()
	dir := t.TempDir()
	// Existing partial recordings must not be overwritten, even without .req.json.
	if err := os.WriteFile(filepath.Join(dir, "1.sse"), []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	recorder := &RecordTransport{Dir: dir}
	client := &http.Client{Transport: recorder}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(request))
			if err != nil {
				t.Error(err)
				return
			}
			req.Header.Set("Authorization", secret)
			resp, err := client.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			got, err := io.ReadAll(resp.Body)
			closeErr := resp.Body.Close()
			if err != nil || closeErr != nil || string(got) != body || resp.StatusCode != http.StatusBadRequest {
				t.Errorf("response changed: %q, %v, %v", got, err, closeErr)
			}
		})
	}
	wg.Wait()
	if err := recorder.Err(); err != nil {
		t.Fatal(err)
	}
	for i := 2; i <= 5; i++ {
		for suffix, want := range map[string]string{".req.json": request, ".sse": body, ".status": "400\n"} {
			path := filepath.Join(dir, fmt.Sprint(i)+suffix)
			got, err := os.ReadFile(path)
			if err != nil || string(got) != want || strings.Contains(string(got), secret) {
				t.Errorf("recording %s: %q, %v", path, got, err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Errorf("recording permissions: %v, %v", info, err)
			}
		}
	}
	if got, err := os.ReadFile(filepath.Join(dir, "1.sse")); err != nil || string(got) != "existing" {
		t.Fatalf("overwrote recording: %q, %v", got, err)
	}
}

func TestRecordTransportCancel(t *testing.T) {
	const partial = "data: first\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, partial)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	dir := t.TempDir()
	client := &http.Client{Transport: &RecordTransport{Dir: dir}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(partial))
	if _, err := io.ReadFull(resp.Body, buf); err != nil || string(buf) != partial {
		t.Fatalf("stream was buffered or changed: %q, %v", buf, err)
	}
	cancel()
	if _, err := io.ReadAll(resp.Body); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel error: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "1.sse"))
	if err != nil || string(got) != partial {
		t.Fatalf("partial response lost: %q, %v", got, err)
	}
}

func TestRecordTransportDirectoryError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(dir, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, "http://unused.invalid", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&RecordTransport{Dir: dir}).RoundTrip(req); err == nil {
		t.Fatal("recording failure was ignored")
	}
	if got, err := os.ReadFile(dir); err != nil || string(got) != "keep" {
		t.Fatalf("existing file changed: %q, %v", got, err)
	}
}

func TestRecordTerminalWriteError(t *testing.T) {
	f, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	recorder := &RecordTransport{}
	body := &recordBody{
		ReadCloser: io.NopCloser(strings.NewReader(sseLines(`{"type":"response.completed","response":{"status":"completed"}}`))),
		file:       f, recorder: recorder,
	}
	defer body.Close()
	stream := responsesStream{yield: func(Event, error) bool { return true }}
	if err := readSSE(body, stream.event); err == nil || stream.stopped {
		t.Fatalf("terminal event hid a recording write failure: stopped=%v err=%v", stream.stopped, err)
	}
	first := recorder.Err()
	if first == nil {
		t.Fatal("recording write failure not retained")
	}
	recorder.recordError(errors.New("later failure"))
	if recorder.Err() != first {
		t.Fatal("first recording failure was replaced")
	}
}

func TestRecordCloseError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "response")
	if err != nil {
		t.Fatal(err)
	}
	recorder := &RecordTransport{}
	body := &recordBody{ReadCloser: io.NopCloser(strings.NewReader("")), file: f, recorder: recorder}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	// Adapters ignore Close's return value; callers must still see the failure.
	body.Close()
	if !errors.Is(recorder.Err(), os.ErrClosed) {
		t.Fatalf("recording close failure lost: %v", recorder.Err())
	}
}
