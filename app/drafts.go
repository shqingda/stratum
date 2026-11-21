package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// drafts keeps what the user writes to each session: leaving a session
// keeps its draft in the kernel, coming back puts it in the composer, and
// a draft the user stops typing in for a second is kept too, so that a
// crash loses none of it.
type drafts struct {
	began bool      // a session has been shown
	of    string    // the session the composer's draft is of
	kept  string    // the draft as the kernel has it
	last  string    // the draft as the last frame saw it
	since time.Time // when it last changed
	// files are the attachments left in each session's composer: they
	// stay with their session, as a Mail draft keeps its attachments.
	files map[string][]attachment
}

const draftPause = time.Second

func (a *App) syncDraft(c *ui.Context) {
	d := &a.drafts
	cur := a.C.Current
	id := ""
	if cur != nil {
		id = cur.ID
	}
	if id != d.of {
		if d.of != "" && a.draft != d.kept {
			a.quiet(a.C.SetDraft(d.of, a.draft))
		}
		if d.files == nil {
			d.files = map[string][]attachment{}
		}
		if d.of != "" || d.began {
			if len(a.attached) > 0 {
				d.files[d.of] = a.attached
			} else {
				delete(d.files, d.of)
			}
			a.attached = d.files[id]
			delete(d.files, id)
		}
		// The first session shown keeps what the composer holds unless
		// the kernel kept something for it; any other gets its own.
		switch {
		case id == "" && d.began:
			a.draft = ""
		case id != "" && (d.began || a.C.Draft != ""):
			a.draft, a.toEnd = a.C.Draft, true
		}
		d.of, d.began = id, d.began || id != ""
		d.kept, d.last, d.since = a.draft, a.draft, c.Now()
		return
	}
	if id == "" || a.draft == d.kept {
		return
	}
	if a.draft != d.last {
		d.last, d.since = a.draft, c.Now()
	}
	if wait := draftPause - c.Now().Sub(d.since); wait > 0 {
		c.After(wait)
		return
	}
	a.quiet(a.C.SetDraft(id, a.draft))
	d.kept = a.draft
}
