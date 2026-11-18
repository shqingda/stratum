package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/shqingda/talos/proto"
)

const automationsKey = "automations:v1"

// StartAutomations starts the background scheduler loop until ctx is cancelled.
func (m *Manager) StartAutomations(ctx context.Context) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				m.tickAutomations(ctx, now)
			}
		}
	}()
}

func (m *Manager) loadAutomations(ctx context.Context) ([]proto.Automation, error) {
	b, err := m.Store.Setting(ctx, automationsKey)
	if err != nil || len(b) == 0 {
		return nil, nil
	}
	var items []proto.Automation
	if err := json.Unmarshal(b, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (m *Manager) saveAutomations(ctx context.Context, items []proto.Automation) error {
	b, err := json.Marshal(items)
	if err != nil {
		return err
	}
	return m.Store.SetSetting(ctx, automationsKey, b)
}

// ListAutomations returns all configured background/scheduled automations.
func (m *Manager) ListAutomations(ctx context.Context) (proto.AutomationList, error) {
	items, err := m.loadAutomations(ctx)
	if err != nil {
		return proto.AutomationList{}, err
	}
	return proto.AutomationList{Items: items}, nil
}

// UpsertAutomation creates or updates an automation and computes its next run time.
func (m *Manager) UpsertAutomation(ctx context.Context, a proto.Automation) (proto.Automation, error) {
	a.Name = strings.TrimSpace(a.Name)
	a.Prompt = strings.TrimSpace(a.Prompt)
	a.Schedule = strings.TrimSpace(a.Schedule)
	if a.Name == "" {
		return proto.Automation{}, proto.Errorf("bad_request", "automation name is required")
	}
	if a.Prompt == "" {
		return proto.Automation{}, proto.Errorf("bad_request", "automation prompt is required")
	}
	if a.Schedule == "" {
		a.Schedule = "@daily"
	}
	next, err := NextCronRun(a.Schedule, time.Now())
	if err != nil {
		return proto.Automation{}, proto.Errorf("bad_request", "invalid schedule %q: %v", a.Schedule, err)
	}
	if a.Mode == "" {
		a.Mode = "auto"
	}
	if a.Cwd == "" {
		if home, herr := os.UserHomeDir(); herr == nil {
			a.Cwd = home
		}
	}
	if a.Enabled {
		a.NextRunAt = next
	} else {
		a.NextRunAt = time.Time{}
	}

	items, err := m.loadAutomations(ctx)
	if err != nil {
		return proto.Automation{}, err
	}
	if a.ID == "" {
		a.ID = newID()
		items = append(items, a)
	} else {
		found := false
		for i := range items {
			if items[i].ID == a.ID {
				a.LastRunAt = items[i].LastRunAt
				a.LastSessionID = items[i].LastSessionID
				a.LastStatus = items[i].LastStatus
				a.LastError = items[i].LastError
				items[i] = a
				found = true
				break
			}
		}
		if !found {
			items = append(items, a)
		}
	}
	if err := m.saveAutomations(ctx, items); err != nil {
		return proto.Automation{}, err
	}
	return a, nil
}

// DeleteAutomation removes an automation by ID.
func (m *Manager) DeleteAutomation(ctx context.Context, id string) error {
	items, err := m.loadAutomations(ctx)
	if err != nil {
		return err
	}
	out := items[:0]
	for _, it := range items {
		if it.ID != id {
			out = append(out, it)
		}
	}
	return m.saveAutomations(ctx, out)
}

// RunAutomationNow executes an automation immediately in a fresh session.
func (m *Manager) RunAutomationNow(ctx context.Context, id string) (proto.Automation, error) {
	items, err := m.loadAutomations(ctx)
	if err != nil {
		return proto.Automation{}, err
	}
	idx := -1
	for i := range items {
		if items[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return proto.Automation{}, proto.Errorf("not_found", "automation %q not found", id)
	}
	return m.executeAutomation(ctx, items, idx, time.Now())
}

func (m *Manager) tickAutomations(ctx context.Context, now time.Time) {
	items, err := m.loadAutomations(ctx)
	if err != nil || len(items) == 0 {
		return
	}
	for i := range items {
		it := items[i]
		if !it.Enabled || it.NextRunAt.IsZero() || now.Before(it.NextRunAt) {
			continue
		}
		_, _ = m.executeAutomation(ctx, items, i, now)
	}
}

func (m *Manager) executeAutomation(ctx context.Context, items []proto.Automation, idx int, now time.Time) (proto.Automation, error) {
	it := items[idx]
	it.LastRunAt = now
	if it.Enabled {
		if next, err := NextCronRun(it.Schedule, now); err == nil {
			it.NextRunAt = next
		}
	}
	mode := it.Mode
	if mode == "" {
		mode = "auto"
	}
	s, err := m.Create(ctx, proto.SessionCreate{
		Title: it.Name,
		Cwd:   it.Cwd,
		Mode:  mode,
	})
	if err != nil {
		it.LastStatus = "error"
		it.LastError = err.Error()
		items[idx] = it
		_ = m.saveAutomations(ctx, items)
		return it, err
	}
	it.LastSessionID = s.ID
	if _, err := m.Prompt(ctx, proto.Prompt{SessionID: s.ID, Text: it.Prompt}, ""); err != nil {
		it.LastStatus = "error"
		it.LastError = err.Error()
	} else {
		it.LastStatus = "ok"
		it.LastError = ""
	}
	items[idx] = it
	_ = m.saveAutomations(ctx, items)
	return it, nil
}

// NextCronRun computes the next run time strictly after from for a schedule
// expression (@hourly, @daily, @weekly, @every <duration>, or 5-field cron).
func NextCronRun(expr string, from time.Time) (time.Time, error) {
	expr = strings.TrimSpace(expr)
	switch {
	case expr == "@hourly":
		expr = "0 * * * *"
	case expr == "@daily" || expr == "@midnight":
		expr = "0 0 * * *"
	case expr == "@weekly":
		expr = "0 0 * * 0"
	case strings.HasPrefix(expr, "@every "):
		d, err := time.ParseDuration(strings.TrimSpace(strings.TrimPrefix(expr, "@every ")))
		if err != nil || d <= 0 {
			return time.Time{}, fmt.Errorf("invalid interval %q", expr)
		}
		return from.Add(d), nil
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return time.Time{}, fmt.Errorf("expected 5 cron fields, got %d", len(fields))
	}
	mins, err := parseCronField(fields[0], 0, 59)
	if err != nil {
		return time.Time{}, err
	}
	hours, err := parseCronField(fields[1], 0, 23)
	if err != nil {
		return time.Time{}, err
	}
	doms, err := parseCronField(fields[2], 1, 31)
	if err != nil {
		return time.Time{}, err
	}
	months, err := parseCronField(fields[3], 1, 12)
	if err != nil {
		return time.Time{}, err
	}
	dows, err := parseCronField(fields[4], 0, 6)
	if err != nil {
		return time.Time{}, err
	}

	t := from.Truncate(time.Minute).Add(time.Minute)
	limit := t.Add(366 * 24 * time.Hour)
	for !t.After(limit) {
		if months[int(t.Month())] && doms[t.Day()] && dows[int(t.Weekday())] && hours[t.Hour()] && mins[t.Minute()] {
			return t, nil
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("no matching time within 1 year for %q", expr)
}

func parseCronField(field string, minVal, maxVal int) (map[int]bool, error) {
	out := make(map[int]bool)
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty cron field")
		}
		step := 1
		if base, stepStr, ok := strings.Cut(part, "/"); ok {
			s, err := strconv.Atoi(stepStr)
			if err != nil || s <= 0 {
				return nil, fmt.Errorf("invalid step %q", part)
			}
			step = s
			part = base
		}
		var lo, hi int
		switch {
		case part == "*":
			lo, hi = minVal, maxVal
		case strings.Contains(part, "-"):
			a, b, _ := strings.Cut(part, "-")
			v1, err1 := strconv.Atoi(a)
			v2, err2 := strconv.Atoi(b)
			if err1 != nil || err2 != nil || v1 < minVal || v2 > maxVal || v1 > v2 {
				return nil, fmt.Errorf("invalid range %q", part)
			}
			lo, hi = v1, v2
		default:
			v, err := strconv.Atoi(part)
			if err != nil || v < minVal || v > maxVal {
				return nil, fmt.Errorf("invalid value %q", part)
			}
			lo, hi = v, v
		}
		for v := lo; v <= hi; v += step {
			out[v] = true
		}
	}
	return out, nil
}
