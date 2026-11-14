// Command talos is the desktop app: the kernel and the window in one
// process, talking over in-process pipes as talosd does over stdio.
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/talos/app"
	"github.com/shqingda/talos/client"
	"github.com/shqingda/talos/kernel/boot"
	"github.com/shqingda/talos/llm/auth"
	"github.com/shqingda/talos/proto"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opts := boot.Options{Creds: &auth.Keychain{}}
	demo := os.Getenv("TALOS_DEMO")
	if demo != "" {
		// The demo keeps out of ~/.talos and the keychain: a fresh data
		// directory and one scripted account.
		dir, err := os.MkdirTemp("", "talos-demo-")
		if err != nil {
			log.Fatal(err)
		}
		defer os.RemoveAll(dir)
		auth.Dir, opts.Creds, opts.Demo = dir, &auth.Memory{}, true
	}
	if err := os.MkdirAll(auth.Dir, 0o700); err != nil {
		log.Fatal(err)
	}
	opts.DB = filepath.Join(auth.Dir, "talos.db")
	k, err := boot.Start(ctx, opts)
	if err != nil {
		log.Fatal(err)
	}
	defer k.Close()

	cwd, _ := os.Getwd()
	if len(os.Args) > 1 {
		cwd, _ = filepath.Abs(os.Args[1])
	}
	if demo != "" {
		cwd = seed(ctx, k, demo, cwd)
	}
	a := app.New(client.New(cwd))
	mygo.Use(native.Plugin) // checks a day apart; builds without updates never do
	mygo.App.WhenReady(func() {
		mygo.App.SetMenu(menu())
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
		a.UseDesktop()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// menu is the default menu bar with "Check for Updates…" after About.
func menu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Talos", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			updater.MenuItem(),
			mygo.Separator(),
			{Role: mygo.RoleServices},
			mygo.Separator(),
			{Role: mygo.RoleHide},
			{Role: mygo.RoleHideOthers},
			{Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Role: mygo.RoleEditMenu},
		{Role: mygo.RoleViewMenu},
		{Role: mygo.RoleWindowMenu},
	})
}

// seed fills a demo's sidebar from scripts/demo.sh's folder: calc is where
// the window opens; site gets a finished answer and notes a turn waiting
// for approval, so the sidebar shows more than one project and state.
func seed(ctx context.Context, k *boot.Kernel, root, cwd string) string {
	if _, err := os.Stat(filepath.Join(root, "calc")); err != nil {
		return cwd
	}
	for _, s := range []struct{ dir, title, text string }{
		{"site", "讲讲这个仓库", "详细解释一下这个仓库"},
		{"notes", "跑一遍测试", "跑测试"},
	} {
		x, err := k.Sessions.Create(ctx, proto.SessionCreate{Cwd: filepath.Join(root, s.dir), Title: s.title})
		if err == nil {
			k.Sessions.Prompt(ctx, proto.Prompt{SessionID: x.ID, Text: s.text}, "")
		}
	}
	return filepath.Join(root, "calc")
}
