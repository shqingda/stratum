// Package rpc serves the protocol over a stream of JSON lines: stdio for
// talosd --rpc, a pipe in tests. The app talks the same commands and
// frames over an in-process channel.
package rpc

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sync"

	"github.com/shqingda/talos/kernel/session"
	"github.com/shqingda/talos/kernel/store"
	ws "github.com/shqingda/talos/kernel/workspace"
	"github.com/shqingda/talos/proto"
)

// Server answers commands for one kernel.
type Server struct {
	Sessions *session.Manager
	// Accounts and Models answer the commands that reach outside the
	// kernel: signed-in accounts and their models.
	Accounts func(ctx context.Context) ([]proto.Account, error)
	Models   func(ctx context.Context, account string) ([]proto.Model, error)
	// Login signs in, telling the user what to do through progress.
	Login func(ctx context.Context, q proto.Login, progress func(proto.LoginProgress)) (proto.Account, error)

	// Logout forgets an account's credential.
	Logout func(ctx context.Context, account string) error

	loginMu     sync.Mutex
	loginCancel context.CancelFunc

	instance string
	once     sync.Once
}

// Instance names this run of the kernel; it changes on restart, so a
// client knows to read snapshots again.
func (s *Server) Instance() string {
	s.once.Do(func() {
		b := make([]byte, 8)
		rand.Read(b)
		s.instance = hex.EncodeToString(b)
	})
	return s.instance
}

// Serve reads commands from r and writes frames to w until r ends or ctx
// does. Every event of every session goes to w.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	out := &writer{enc: json.NewEncoder(w)}
	out.send(proto.Frame{Type: proto.FrameReady, Version: proto.Version, Instance: s.Instance()})
	unsub := s.Sessions.Subscribe(func(e proto.Event) {
		out.send(proto.Frame{Type: proto.FrameEvent, Event: &e})
	})
	defer unsub()

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), proto.MaxLine)
	var wg sync.WaitGroup
	defer wg.Wait()
	for sc.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var cmd proto.Command
		if err := json.Unmarshal(sc.Bytes(), &cmd); err != nil {
			out.send(proto.Frame{Type: proto.FrameResponse, Error: proto.Errorf("bad_request", "not a command: %v", err)})
			continue
		}
		// Commands run concurrently: a slow one must not hold up cancel.
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := s.handle(ctx, cmd)
			f := proto.Frame{Type: proto.FrameResponse, ID: cmd.ID}
			if err != nil {
				var pe *proto.Error
				if !errors.As(err, &pe) {
					pe = proto.Errorf("internal", "%v", err)
				}
				f.Error = pe
			} else {
				if result == nil {
					result = struct{}{}
				}
				f.Result, _ = json.Marshal(result)
			}
			out.send(f)
		}()
	}
	return sc.Err()
}

type writer struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func (w *writer) send(f proto.Frame) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.enc.Encode(f)
}

func params[T any](cmd proto.Command) (T, error) {
	var v T
	if len(cmd.Params) == 0 {
		return v, nil
	}
	if err := json.Unmarshal(cmd.Params, &v); err != nil {
		return v, proto.Errorf("bad_request", "%s: %v", cmd.Method, err)
	}
	return v, nil
}

func (s *Server) handle(ctx context.Context, cmd proto.Command) (any, error) {
	m := s.Sessions
	switch cmd.Method {
	case proto.MethodInitialize:
		return proto.Hello{Version: proto.Version, Instance: s.Instance()}, nil
	case proto.MethodSessionCreate:
		p, err := params[proto.SessionCreate](cmd)
		if err != nil {
			return nil, err
		}
		return m.Create(ctx, p)
	case proto.MethodSessionList:
		return m.List(ctx)
	case proto.MethodSessionGet:
		p, err := params[proto.SessionRef](cmd)
		if err != nil {
			return nil, err
		}
		return m.Get(ctx, p.SessionID)
	case proto.MethodSessionUpdate:
		p, err := params[proto.SessionUpdate](cmd)
		if err != nil {
			return nil, err
		}
		return m.Update(ctx, p)
	case proto.MethodSessionFlag:
		p, err := params[proto.SessionFlag](cmd)
		if err != nil {
			return nil, err
		}
		return m.Flag(ctx, p)
	case proto.MethodSessionDelete:
		p, err := params[proto.SessionRef](cmd)
		if err != nil {
			return nil, err
		}
		return nil, m.Delete(ctx, p.SessionID)
	case proto.MethodPrompt:
		p, err := params[proto.Prompt](cmd)
		if err != nil {
			return nil, err
		}
		return m.Prompt(ctx, p, cmd.ID)
	case proto.MethodShell:
		p, err := params[proto.ShellRun](cmd)
		if err != nil {
			return nil, err
		}
		return nil, m.Shell(ctx, p)
	case proto.MethodDraftSet:
		p, err := params[proto.DraftSet](cmd)
		if err != nil {
			return nil, err
		}
		return nil, m.SetDraft(ctx, p)
	case proto.MethodRewind:
		p, err := params[proto.Rewind](cmd)
		if err != nil {
			return nil, err
		}
		return m.Rewind(ctx, p)
	case proto.MethodQueueEdit:
		p, err := params[proto.QueueEdit](cmd)
		if err != nil {
			return nil, err
		}
		return nil, m.EditQueue(p)
	case proto.MethodCancel:
		p, err := params[proto.SessionRef](cmd)
		if err != nil {
			return nil, err
		}
		m.Cancel(p.SessionID)
		return nil, nil
	case proto.MethodEvents:
		p, err := params[proto.EventsQuery](cmd)
		if err != nil {
			return nil, err
		}
		// --- 任务4 ---
		if p.Before != 0 {
			return s.Sessions.EventsBefore(ctx, p)
		}
		limit := p.Limit
		if limit <= 0 || limit > 2000 {
			limit = 500
		}
		evs, err := m.Store.Events(ctx, p.SessionID, p.After, limit+1)
		if err != nil {
			return nil, err
		}
		page := proto.EventPage{Events: []proto.Event{}}
		if len(evs) > limit {
			evs, page.More = evs[:limit], true
		}
		for _, e := range evs {
			page.Events = append(page.Events, proto.Event{SessionID: e.SessionID, Seq: e.Seq, Type: e.Type, Data: e.Data, At: e.At})
		}
		return page, nil
	case proto.MethodApprovalRespond:
		p, err := params[proto.ApprovalAnswer](cmd)
		if err != nil {
			return nil, err
		}
		return nil, m.Answer(p)
	case proto.MethodAccounts:
		if s.Accounts == nil {
			return []proto.Account{}, nil
		}
		return s.Accounts(ctx)
	case proto.MethodModels:
		p, err := params[proto.ModelsQuery](cmd)
		if err != nil {
			return nil, err
		}
		if s.Models == nil {
			return []proto.Model{}, nil
		}
		return s.Models(ctx, p.Account)
	case proto.MethodReceipt:
		p, err := params[proto.ReceiptRef](cmd)
		if err != nil {
			return nil, err
		}
		r, err := m.Store.GetReceipt(ctx, p.ID)
		if errors.Is(err, store.ErrNotFound) {
			return proto.Receipt{ID: p.ID, Status: "unknown"}, nil
		}
		if err != nil {
			return nil, err
		}
		return proto.Receipt{ID: r.ID, Method: r.Method, Status: r.Status, Error: r.Error}, nil
	}
	// --- 任务3 ---
	if result, err, handled := s.handleChanges(ctx, cmd); handled {
		return result, err
	}

	// --- 任务4 ---
	if cmd.Method == proto.MethodSearch {
		return s.search(ctx, cmd)
	}
	if cmd.Method == proto.MethodFilesFind {
		p, err := params[proto.FilesQuery](cmd)
		if err != nil {
			return nil, err
		}
		if p.Limit <= 0 {
			p.Limit = 20
		}
		return ws.Match(withDirs(listed(ctx, p.Cwd)), p.Query, p.Limit), nil
	}
	if cmd.Method == proto.MethodWorkspace {
		p, err := params[proto.WorkspaceQuery](cmd)
		if err != nil {
			return nil, err
		}
		return workspace(ctx, p.Cwd)
	}
	if cmd.Method == proto.MethodWorkspaceDiff {
		p, err := params[proto.WorkspaceQuery](cmd)
		if err != nil {
			return nil, err
		}
		return workspaceDiff(ctx, p)
	}
	switch cmd.Method {
	case proto.MethodLogin:
		p, err := params[proto.Login](cmd)
		if err != nil {
			return nil, err
		}
		return s.login(ctx, p)
	case proto.MethodLoginCancel:
		s.cancelLogin(nil)
		return struct{}{}, nil
	case proto.MethodLogout:
		p, err := params[proto.AccountRef](cmd)
		if err != nil {
			return nil, err
		}
		if s.Logout == nil {
			return nil, proto.Errorf("bad_request", "sign-out is not available")
		}
		return struct{}{}, s.Logout(ctx, p.Account)
	case proto.MethodSettings:
		return s.Sessions.Settings(ctx)
	case proto.MethodMCPList:
		p, err := params[proto.MCPQuery](cmd)
		if err != nil {
			return nil, err
		}
		return s.Sessions.MCPList(ctx, p)
	case proto.MethodMCPTrust:
		p, err := params[proto.MCPTrust](cmd)
		if err != nil {
			return nil, err
		}
		return s.Sessions.MCPTrust(ctx, p)
	case proto.MethodSetSettings:
		p, err := params[proto.Settings](cmd)
		if err != nil {
			return nil, err
		}
		return s.Sessions.SetSettings(ctx, p)
	}
	if cmd.Method == proto.MethodCommit {
		p, err := params[proto.Commit](cmd)
		if err != nil {
			return nil, err
		}
		return commit(ctx, p)
	}
	return nil, proto.Errorf("bad_request", "unknown method %q", cmd.Method)
}

// --- 任务3 ---

func (s *Server) handleChanges(ctx context.Context, cmd proto.Command) (any, error, bool) {
	switch cmd.Method {
	case proto.MethodChangesList:
		p, err := params[proto.SessionRef](cmd)
		if err != nil {
			return nil, err, true
		}
		result, err := s.Sessions.ChangesList(ctx, p.SessionID)
		return result, err, true
	case proto.MethodChangesDiff, proto.MethodChangesRevert, proto.MethodChangesRedo:
		p, err := params[proto.ChangeQuery](cmd)
		if err != nil {
			return nil, err, true
		}
		if cmd.Method == proto.MethodChangesDiff {
			result, err := s.Sessions.ChangesDiff(ctx, p)
			return result, err, true
		}
		result, err := s.Sessions.ChangesApply(ctx, p, cmd.Method == proto.MethodChangesRedo)
		return result, err, true
	case proto.MethodChangesRevertHunk:
		p, err := params[proto.HunkQuery](cmd)
		if err != nil {
			return nil, err, true
		}
		result, err := s.Sessions.ChangesRevertHunk(ctx, p)
		return result, err, true
	}
	return nil, nil, false
}

// --- 任务4 ---

func (s *Server) search(ctx context.Context, cmd proto.Command) (any, error) {
	p, err := params[proto.SearchQuery](cmd)
	if err != nil {
		return nil, err
	}
	return s.Sessions.Search(ctx, p)
}

// login runs one sign-in at a time: a new one ends the last.
func (s *Server) login(ctx context.Context, q proto.Login) (proto.Account, error) {
	if s.Login == nil {
		return proto.Account{}, proto.Errorf("bad_request", "sign-in is not available")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.cancelLogin(cancel)
	a, err := s.Login(ctx, q, func(p proto.LoginProgress) { s.Sessions.Broadcast(proto.EventLogin, p) })
	if ctx.Err() != nil {
		return a, proto.Errorf("cancelled", "sign-in cancelled")
	}
	return a, err
}

func (s *Server) cancelLogin(next context.CancelFunc) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	if s.loginCancel != nil {
		s.loginCancel()
	}
	s.loginCancel = next
}
