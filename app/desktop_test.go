package app

import (
	"reflect"
	"testing"

	"github.com/shqingda/talos/proto"
)

type fakeDesk struct {
	active bool
	badges []int
	notes  []string
}

func (d *fakeDesk) Active() bool { return d.active }
func (d *fakeDesk) Badge(n int)  { d.badges = append(d.badges, n) }
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
