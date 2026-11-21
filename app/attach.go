package app

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/llm"
	"github.com/shqingda/talos/locales"
)

// maxImage is the largest picture sent inline; a larger one goes as a
// path the model opens itself.
const maxImage = 3_500_000

var imageTypes = map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}

// attachment is a picture sent with the prompt, or a file it names.
type attachment struct {
	name  string
	path  string
	img   *llm.Image
	thumb ui.ImageSource
}

// attachPaths adds files: pictures inline, the rest by path.
func (a *App) attachPaths(paths []string) {
	for _, path := range paths {
		if slicesHas(a.attached, path) {
			continue
		}
		at := attachment{name: filepath.Base(path), path: path}
		if mt, ok := imageTypes[strings.ToLower(filepath.Ext(path))]; ok {
			if b, err := os.ReadFile(path); err == nil && len(b) <= maxImage {
				at.img = &llm.Image{MediaType: mt, Data: b}
				at.thumb, _ = ui.DecodeImage(b)
			}
		}
		a.attached = append(a.attached, at)
	}
}

func slicesHas(list []attachment, path string) bool {
	for _, at := range list {
		if at.path == path {
			return true
		}
	}
	return false
}

// attachPNG adds a pasted picture.
func (a *App) attachPNG(b []byte) {
	if len(b) > maxImage {
		a.err = locales.ImageTooLarge()
		return
	}
	thumb, _ := ui.DecodeImage(b)
	a.attached = append(a.attached, attachment{name: locales.PastedImage(), img: &llm.Image{MediaType: "image/png", Data: b}, thumb: thumb})
}

// paste takes files or a picture on the clipboard as attachments; text
// pastes as usual.
func (a *App) paste(svc ui.Services) bool {
	data := svc.ReadClipboardData(transfer.FileList, transfer.PNG)
	if b, err := data.Read(transfer.FileList); err == nil {
		var paths []string
		for _, line := range strings.Split(string(b), "\n") {
			if u, err := url.Parse(strings.TrimSpace(line)); err == nil && u.Scheme == "file" {
				paths = append(paths, u.Path)
			}
		}
		if len(paths) > 0 {
			a.attachPaths(paths)
			return true
		}
	}
	if b, err := data.Read(transfer.PNG); err == nil {
		a.attachPNG(b)
		return true
	}
	return false
}

// pickFiles is the system's open panel, for any number of files.
func pickFiles() []string {
	paths, _ := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: locales.AttachAdd(), Multiple: true})
	return paths
}

// choose asks for files with the system's open panel.
func (a *App) choose() {
	if a.pick == nil || a.update == nil {
		return
	}
	go func() {
		if paths := a.pick(); len(paths) > 0 {
			a.update(func() { a.attachPaths(paths) })
		}
	}()
}

// prompt is the draft with the files it names, and the pictures it sends.
func (a *App) prompt() (string, []llm.Image) {
	text := a.expandCommand(strings.TrimSpace(a.draft))
	if len(a.mentionSessions) > 0 {
		refs := make([]string, len(a.mentionSessions))
		for i, id := range a.mentionSessions {
			refs[i] = "@session:" + id
		}
		prefix := strings.Join(refs, " ")
		if text != "" {
			text = prefix + " " + text
		} else {
			text = prefix
		}
	}
	var files []string
	var images []llm.Image
	for _, at := range a.attached {
		if at.img != nil {
			images = append(images, *at.img)
		} else {
			files = append(files, at.path)
		}
	}
	if len(files) > 0 {
		if text != "" {
			text += "\n\n"
		}
		text += locales.AttachmentsHeader() + "\n" + strings.Join(files, "\n")
	}
	return text, images
}

// attachments are the composer's chips, one for each attachment.
func (a *App) attachments(c *ui.Context) {
	p := a.p
	if len(a.attached) == 0 {
		return
	}
	ui.Row(c).Wrap().Gap(6).Margin(0, 0, 8, -4).Children(func() {
		for i, at := range a.attached {
			ui.Row(c.Key(i)).Height(36).Padding(3, 4, 3, 3).Gap(7).Radius(8).AlignItems(ui.Center).Background(p.Chip).MaxWidth(220).Children(func() {
				ui.Box(c).Size(30, 30).Radius(6).Clip().Background(p.Card).Center().Shrink(0).Children(func() {
					if at.thumb != nil {
						ui.Image(c, at.thumb).Size(30, 30).Fit(ui.Cover)
					} else {
						sym(c, "file", 15, p.Fg2)
					}
				})
				ui.Text(c, at.name).FontSize(13).TextColor(p.Fg2).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
				if a.iconButton(c, fmt.Sprint("unattach:", i), "x", locales.AttachRemove(at.name)).Size(24, 24).Clicked() {
					a.attached = append(a.attached[:i:i], a.attached[i+1:]...)
				}
			})
		}
	})
}

// noImages says the session's model may not see pictures it is sent.
func (a *App) noImages() bool {
	cur := a.C.Current
	if cur == nil {
		return false
	}
	has := false
	for _, at := range a.attached {
		has = has || at.img != nil
	}
	for _, m := range a.C.Models {
		if m.ID == cur.Model {
			return has && !m.Images
		}
	}
	return false
}

// sentImages are the pictures of a user message, small.
func (a *App) sentImages(c *ui.Context, e *client.Entry) {
	if a.thumbs == nil || len(a.thumbs) > 64 {
		a.thumbs = map[string]ui.ImageSource{}
	}
	ui.Row(c).Wrap().Gap(6).Children(func() {
		for i := range e.Images {
			key := fmt.Sprint(e.Key, ":", i)
			src, ok := a.thumbs[key]
			if !ok {
				src, _ = ui.DecodeImage(e.Images[i].Data)
				a.thumbs[key] = src
			}
			if src == nil {
				continue
			}
			w, h := ui.ImageSize(src)
			if h > 0 {
				w = min(w*120/h, 240)
			}
			ui.Image(c.Key(i), src).Size(w, 120).Fit(ui.Cover).Radius(8).Border(.5, a.p.Edge2)
		}
	})
}
