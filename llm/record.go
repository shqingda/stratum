package llm

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
)

// RecordTransport records request bodies, response bytes as they are read, and
// status codes. Headers are never recorded. Files are private and never replaced.
type RecordTransport struct {
	Dir  string
	Base http.RoundTripper // http.DefaultTransport when nil
	next atomic.Uint64
	mu   sync.Mutex
	err  error
}

// Err returns the first recording failure, including a response Close error
// that the adapter may not observe.
func (t *RecordTransport) Err() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

func (t *RecordTransport) recordError(err error) error {
	if err != nil {
		t.mu.Lock()
		if t.err == nil {
			t.err = err
		}
		t.mu.Unlock()
	}
	return err
}

func (t *RecordTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		err = errors.Join(err, req.Body.Close())
		if err != nil {
			return nil, err
		}
		req = req.Clone(req.Context())
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	files, err := t.reserve()
	if err != nil {
		return nil, t.recordError(err)
	}
	_, err = files[0].Write(body)
	err = errors.Join(err, files[0].Close())
	if err != nil {
		files[1].Close()
		files[2].Close()
		return nil, t.recordError(err)
	}
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil {
		files[1].Close()
		files[2].Close()
		return nil, err
	}
	_, err = fmt.Fprintln(files[2], resp.StatusCode)
	err = errors.Join(err, files[2].Close())
	if err != nil {
		resp.Body.Close()
		files[1].Close()
		return nil, t.recordError(err)
	}
	resp.Body = &recordBody{ReadCloser: resp.Body, file: files[1], recorder: t}
	return resp, nil
}

func (t *RecordTransport) reserve() ([3]*os.File, error) {
	if err := os.MkdirAll(t.Dir, 0o700); err != nil {
		return [3]*os.File{}, err
	}
	for {
		prefix := filepath.Join(t.Dir, strconv.FormatUint(t.next.Add(1), 10))
		var files [3]*os.File
		var err error
		for i, suffix := range []string{".req.json", ".sse", ".status"} {
			files[i], err = os.OpenFile(prefix+suffix, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				for _, f := range files[:i] {
					f.Close()
					os.Remove(f.Name())
				}
				break
			}
		}
		if err == nil {
			return files, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return [3]*os.File{}, err
		}
	}
}

type recordBody struct {
	io.ReadCloser
	file     *os.File
	recorder *RecordTransport
}

func (b *recordBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		_, writeErr := b.file.Write(p[:n])
		if writeErr != nil {
			// Do not let a terminal event hide a failed recording of this chunk.
			return 0, errors.Join(err, b.recorder.recordError(writeErr))
		}
	}
	return n, err
}

func (b *recordBody) Close() error {
	return errors.Join(b.ReadCloser.Close(), b.recorder.recordError(b.file.Close()))
}
