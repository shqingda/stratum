package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// sseEvent is one server-sent event.
type sseEvent struct {
	Name string
	Data []byte
}

// readSSE calls fn for each event of r until r ends, fn returns false or an
// error, or a line exceeds 8 MiB.
func readSSE(r io.Reader, fn func(sseEvent) (bool, error)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	var name string
	var data bytes.Buffer
	dispatch := func() (bool, error) {
		if data.Len() == 0 && name == "" {
			return true, nil
		}
		ev := sseEvent{Name: name, Data: bytes.TrimSuffix(data.Bytes(), []byte("\n"))}
		name = ""
		data.Reset()
		return fn(ev)
	}
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			if ok, err := dispatch(); !ok || err != nil {
				return err
			}
			continue
		}
		if line[0] == ':' {
			continue
		}
		field, value, _ := bytes.Cut(line, []byte(":"))
		value = bytes.TrimPrefix(value, []byte(" "))
		switch string(field) {
		case "event":
			name = string(value)
		case "data":
			data.Write(value)
			data.WriteByte('\n')
		}
	}
	if err := sc.Err(); err != nil {
		return &Error{Message: "reading stream: " + err.Error(), Retryable: true}
	}
	_, err := dispatch()
	return err
}

// client is what adapters send requests with.
type client struct {
	HTTP *http.Client
}

func (c client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// post sends body as JSON and returns the response when it is 2xx; any
// other status becomes an *Error.
func (c client) post(ctx context.Context, url string, header http.Header, body any) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header = header.Clone()
	req.Header.Set("Content-Type", "application/json")
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "text/event-stream")
	}
	resp, err := c.http().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &Error{Message: err.Error(), Retryable: true}
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return nil, httpError(resp.StatusCode, msg)
	}
	return resp, nil
}

// httpError turns an error response into an *Error, pulling the message out
// of the JSON bodies providers send.
func httpError(status int, body []byte) *Error {
	var parsed struct {
		Error json.RawMessage `json:"error"`
	}
	msg := strings.TrimSpace(string(body))
	if json.Unmarshal(body, &parsed) == nil && len(parsed.Error) > 0 {
		var obj struct {
			Message string `json:"message"`
		}
		var s string
		if json.Unmarshal(parsed.Error, &obj) == nil && obj.Message != "" {
			msg = obj.Message
		} else if json.Unmarshal(parsed.Error, &s) == nil {
			msg = s
		}
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	low := strings.ToLower(msg)
	return &Error{
		Status:    status,
		Message:   msg,
		Retryable: status == 408 || status == 409 || status == 429 || status >= 500,
		ContextOverflow: (status == 400 || status == 413) && (strings.Contains(low, "context") || strings.Contains(low, "too long") ||
			strings.Contains(low, "maximum") && strings.Contains(low, "token")),
	}
}

// streamError is an error a provider reported in the middle of a stream.
func streamError(kind, msg string) *Error {
	k := strings.ToLower(kind)
	return &Error{
		Message:   fmt.Sprintf("%s: %s", kind, msg),
		Retryable: strings.Contains(k, "overload") || strings.Contains(k, "rate") || strings.Contains(k, "server") || strings.Contains(k, "timeout"),
	}
}
