package app

import (
	"reflect"
	"testing"

	"github.com/shqingda/stratum/proto"
)

type fakeDesk struct {
	active bool
	badges []int
	notes  []string
	awake  []bool
}

func (d *fakeDesk) Active() bool      { return d.active }
func (d *fakeDesk) Badge(n int)       { d.badges = append(d.badges, n) }
func (d *fakeDesk) SetAwake(on bool)  { d.awake = append(d.awake, on) }
func (d *fakeDesk) Appearance(string) {}
func (d *fakeDesk) Notify(id, title, body string) {
	d.notes = append(d.notes, id+" "+body)
}

func TestDesktop(t *testing.T) {
	a := working()
	d := &fakeDesk{}
	a.desk = d
	a.C.Approvals = nil
	a.C.Marks["2"] = "run"
	a.C.Marks["3"] = "wait"
	a.watch() // what is already so when the app starts is not news
	if len(d.notes) != 0 || !reflect.DeepEqual(d.badges, []int{1}) {
		t.Fatalf("start: %v %v", d.notes, d.badges)
	}

	a.C.Marks["2"] = "wait"
	a.C.Approvals = []proto.Approval{{ID: "x"}} // the current session waits too
	a.watch()
	a.C.Marks["2"] = "fail"
	a.C.Marks["3"] = "run"
	a.watch()
	a.C.Marks["3"] = "unread"
	a.watch()
	want := []string{"1 等你确认", "2 等你确认", "3 这一轮做完了"}
	if !reflect.DeepEqual(d.notes, want) || !reflect.DeepEqual(d.badges, []int{1, 3, 1}) {
		t.Fatalf("notes %v badges %v", d.notes, d.badges)
	}

	// In front, the sidebar shows it; only the badge follows.
	d.active = true
	a.C.Approvals = nil
	a.C.Busy = false
	a.C.Marks["4"] = "wait"
	a.watch()
	if len(d.notes) != 3 || d.badges[len(d.badges)-1] != 1 {
		t.Fatalf("active: %v %v", d.notes, d.badges)
	}
}

func TestDesktopPreventSleepWhileRunning(t *testing.T) {
	a := working()
	d := &fakeDesk{}
	a.desk = d
	a.C.Approvals = nil
	a.C.Busy = true
	a.C.Marks["1"] = "run"
	a.C.Marks["2"] = ""
	a.C.Marks["3"] = ""

	// Disabled by default: running session does not call SetAwake.
	a.watch()
	if len(d.awake) != 0 {
		t.Fatalf("expected no SetAwake when PreventSleep is false, got %v", d.awake)
	}

	// Enabled: running -> true, waiting for approval -> false, running -> true, done -> false.
	a.C.Settings = &proto.Settings{PreventSleep: true}
	a.watch()
	a.C.Busy = false
	a.C.Marks["1"] = "wait"
	a.watch()
	a.C.Busy = true
	a.C.Marks["1"] = "run"
	a.watch()
	a.C.Busy = false
	a.C.Marks["1"] = ""
	a.watch()

	want := []bool{true, false, true, false}
	if !reflect.DeepEqual(d.awake, want) {
		t.Fatalf("SetAwake sequence = %v, want %v", d.awake, want)
	}
}
