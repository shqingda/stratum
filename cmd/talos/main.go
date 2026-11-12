// Command talos is the desktop app: the kernel and the window in one
// process, talking over in-process pipes as talosd does over stdio.
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/app"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/kernel/boot"
	"github.com/shqingda/talos/llm/auth"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := os.MkdirAll(auth.Dir, 0o700); err != nil {
		log.Fatal(err)
	}
	k, err := boot.Start(ctx, boot.Options{DB: filepath.Join(auth.Dir, "talos.db"), Creds: &auth.Keychain{}})
	if err != nil {
		log.Fatal(err)
	}
	defer k.Close()

	cwd, _ := os.Getwd()
	if len(os.Args) > 1 {
		cwd, _ = filepath.Abs(os.Args[1])
	}
	a := app.New(client.New(cwd))
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:          "Talos",
			Width:          1200,
			Height:         780,
			MinWidth:       760,
			MinHeight:      480,
			StateKey:       "main",
			TitleBarStyle:  mygo.TitleBarHiddenInset,
			TitleBarHeight: 52,
			Vibrancy:       mygo.VibrancySidebar,
			Content:        ui.View(a.View),
		})
		a.Serve(ctx, k.Server, win.Update)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
