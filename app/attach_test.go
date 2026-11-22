package app

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/llm"
	"github.com/shqingda/stratum/proto"
)

func testPNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(60 + x*120/w), uint8(90 + y*100/h), 160, 255})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func TestAttachments(t *testing.T) {
	dir := t.TempDir()
	shot, notes := filepath.Join(dir, "报错截图.png"), filepath.Join(dir, "notes.pdf")
	os.WriteFile(shot, testPNG(64, 40), 0o644)
	os.WriteFile(notes, []byte("%PDF"), 0o644)

	a := working()
	a.C.Queued = nil
	a.out = make(chan []proto.Command, 16)
	a.attachPaths([]string{shot, notes, shot})
	if len(a.attached) != 2 || a.attached[0].img == nil || a.attached[1].img != nil {
		t.Fatalf("%+v", a.attached)
	}
	a.draft = "看下这个报错"
	tt := ui.NewTester(a.View, 1200, 780)
	tt.Frame()
	if !tt.HasText("报错截图.png") || !tt.HasText("notes.pdf") {
		t.Fatal("chips not shown")
	}
	snapshot(t, tt, "attachments")

	if err := tt.Click("移除 notes.pdf"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	a.attachPaths([]string{notes})
	a.submit()
	var p proto.Prompt
	for cmds := range a.out {
		for _, cmd := range cmds {
			if cmd.Method == proto.MethodPrompt {
				json.Unmarshal(cmd.Params, &p)
			}
		}
		break
	}
	if len(p.Images) != 1 || p.Images[0].MediaType != "image/png" || !strings.HasSuffix(p.Text, "附件：\n"+notes) || !strings.HasPrefix(p.Text, "看下这个报错") {
		t.Fatalf("%q %d", p.Text, len(p.Images))
	}
	if a.draft != "" || a.attached != nil {
		t.Fatal("composer not cleared")
	}
}

func TestSentImages(t *testing.T) {
	a := working()
	for _, e := range a.C.Entries {
		if e.Role == client.User {
			e.Images = append(e.Images, llm.Image{MediaType: "image/png", Data: testPNG(160, 100)})
			break
		}
	}
	tt := ui.NewTester(a.View, 1200, 780)
	tt.Frame()
	snapshot(t, tt, "sent-images")
}
