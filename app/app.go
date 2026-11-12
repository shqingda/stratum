// Package app is Talos's window: a sidebar of sessions, the transcript of
// the one shown and the composer under it, drawn with mygo's native UI
// over a client.Client.
package app

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/kernel/rpc"
	"github.com/shqingda/talos/proto"
)

//go:embed assets/mark.png
var markPNG []byte

// mark is the app icon, drawn from a PNG: mygo's SVG leaves out the
// gradients and clips the icon is made of.
var mark = func() ui.ImageSource {
	src, err := ui.DecodeImage(markPNG)
	if err != nil {
		panic(err)
	}
	return src
}()

// App is the state of the window. Everything in it is touched on the UI
// thread only: frames from the kernel come in through update.
type App struct {
	C *client.Client

	out    chan []proto.Command
	p      *Palette
	now    func() time.Time
	err    string       // the last command that could not be sent
	draft  string       // what the composer holds
	query  string       // the sidebar's search
	list   ui.ListState // the transcript
	md     markdown.Options
	opened map[string]bool // tool calls whose output shows
}

// New returns an app over c, not connected yet.
func New(c *client.Client) *App {
	return &App{C: c, now: time.Now, opened: map[string]bool{}}
}

// Serve connects the app to a kernel on in-process pipes until ctx ends.
// update runs a function on the UI thread and draws again after it, as
// mygo.Window.Update does.
func (a *App) Serve(ctx context.Context, srv *rpc.Server, update func(func())) {
	cr, cw := io.Pipe()
	fr, fw := io.Pipe()
	a.out = make(chan []proto.Command, 256)
	go func() { srv.Serve(ctx, cr, fw); fw.Close() }()
	// Commands go out on their own goroutine, so that the UI thread never
	// waits on a pipe the kernel is not reading yet.
	go func() {
		for {
			select {
			case cmds := <-a.out:
				for _, cmd := range cmds {
					b, _ := json.Marshal(cmd)
					if _, err := cw.Write(append(b, '\n')); err != nil {
						return
					}
				}
			case <-ctx.Done():
				cw.Close()
				return
			}
		}
	}()
	go func() {
		sc := bufio.NewScanner(fr)
		sc.Buffer(nil, proto.MaxLine)
		for sc.Scan() {
			var f proto.Frame
			if json.Unmarshal(sc.Bytes(), &f) != nil {
				continue
			}
			update(func() { a.send(a.C.Receive(f)) })
		}
		err := sc.Err()
		if err == nil {
			err = io.EOF
		}
		update(func() { a.C.Disconnected(err) })
	}()
}

func (a *App) send(cmds []proto.Command) {
	if len(cmds) > 0 && a.out != nil {
		a.out <- cmds
	}
}

// do sends what an action of the user's returned, or keeps its error to
// show.
func (a *App) do(cmds []proto.Command, err error) {
	if err != nil {
		a.err = err.Error()
		return
	}
	a.err = ""
	a.send(cmds)
}

// View builds a frame of the window.
func (a *App) View(c *ui.Context) {
	isDark := c.Theme().Dark
	a.p = PaletteFor(isDark)
	c.SetTheme(Theme(a.p, isDark))
	vibrant := c.Vibrancy()
	if vibrant {
		c.Root().Background(ui.Transparent)
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		a.sidebar(c, vibrant)
		a.main(c)
	})
}

func (a *App) main(c *ui.Context) {
	p := a.p
	ui.Column(c).Grow(1).MinWidth(0).Background(p.Bg).Children(func() {
		a.bar(c)
		a.transcript(c)
		a.composer(c)
	})
}
