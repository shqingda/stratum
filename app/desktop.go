package app

import (
	"github.com/egoist/mygo/plugins/updater"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/shqingda/talos/locales"
)

// Desktop is what the app tells the system about its sessions: a Dock
// badge with how many wait on the user, and a notification when one
// starts waiting or finishes while the app is in the background.
type Desktop interface {
	Active() bool
	Badge(n int)
	Notify(id, title, body string)
	// Appearance shows the app light, dark, or as the system is ("").
	Appearance(theme string)
}

// Locale is the language the app shows: the one chosen, or "" to follow
// the system.
type Locale interface {
	Chosen() string
	Set(tag string)
}

// memLocale keeps the choice for an app with no system behind it.
type memLocale struct{ tag string }

func (l *memLocale) Chosen() string { return l.tag }
func (l *memLocale) Set(tag string) { l.tag = tag }

// systemLocale is mygo's, which keeps the choice for the next launches.
type systemLocale struct{}

func (systemLocale) Chosen() string { return mygo.App.ChosenLocale() }
func (systemLocale) Set(tag string) { mygo.App.SetLocale(tag) }

// system is the Desktop of the running app.
type system struct{}

func (system) Active() bool { return mygo.App.IsActive() }
func (system) Badge(n int)  { mygo.App.SetBadgeCount(n) }

func (system) Appearance(theme string) {
	src := mygo.ThemeSystem
	switch theme {
	case "light":
		src = mygo.ThemeLight
	case "dark":
		src = mygo.ThemeDark
	}
	mygo.Theme.SetSource(src)
}

func (system) Notify(id, title, body string) {
	if !mygo.NotificationsSupported() {
		return
	}
	mygo.NewNotification(mygo.NotificationOptions{ID: notePrefix + id, Title: title, Body: body, Group: id}).Show()
}

const notePrefix = "session:"

// UseDesktop shows the sessions' states on the Dock and in notifications;
// clicking a notification opens its session.
func (a *App) UseDesktop() {
	a.desk, a.locale = system{}, systemLocale{}
	a.version, a.checkUpdates = mygo.App.Version(), updater.CheckForUpdates
	mygo.App.OnNotificationClick(func(id string) {
		sid, ok := strings.CutPrefix(id, notePrefix)
		if !ok || a.update == nil {
			return
		}
		mygo.App.Focus()
		a.update(func() { a.do(a.C.Open(sid)) })
	})
}

// watch compares the sessions' states with the last ones seen, after a
// frame from the kernel.
func (a *App) watch() {
	if a.desk == nil {
		return
	}
	first := a.marks == nil
	if first {
		a.marks = map[string]string{}
	}
	set := a.settings()
	if set.Theme != a.theme {
		a.theme = set.Theme
		a.desk.Appearance(set.Theme)
	}
	quiet := func(kind string) bool { return slices.Contains(set.Quiet, kind) }
	waiting := 0
	active := !first && a.desk.Active() && set.NotifyWhen != "always"
	for _, s := range a.C.Sessions {
		st := a.state(s)
		if st == "wait" {
			waiting++
		}
		was, known := a.marks[s.ID]
		a.marks[s.ID] = st
		if first || !known || active || st == was || set.NotifyWhen == "never" {
			continue
		}
		switch {
		case st == "wait" && !quiet("wait"):
			a.desk.Notify(s.ID, title(s), locales.NotifyWait())
		case was == "run" && st == "fail" && !quiet("fail"):
			a.desk.Notify(s.ID, title(s), locales.NotifyFail())
		case was == "run" && (st == "unread" || st == "") && !quiet("done"):
			a.desk.Notify(s.ID, title(s), locales.NotifyDone())
		}
	}
	if quiet("badge") {
		waiting = 0
	}
	if waiting != a.badge {
		a.badge = waiting
		a.desk.Badge(waiting)
	}
}
