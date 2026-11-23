// Command stratum is the desktop app: the kernel and the window in one
// process, talking over in-process pipes as stratumd does over stdio.
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/permissionflow"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
	"github.com/shqingda/stratum/app"
	"github.com/shqingda/stratum/app/web"
	"github.com/shqingda/stratum/client"
	"github.com/shqingda/stratum/kernel/boot"
	"github.com/shqingda/stratum/kernel/computer"
	"github.com/shqingda/stratum/kernel/preview"
	"github.com/shqingda/stratum/llm/auth"
	"github.com/shqingda/stratum/locales"
	"github.com/shqingda/stratum/proto"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opts := boot.Options{Creds: &auth.Keychain{}}
	demo := os.Getenv("STRATUM_DEMO")
	if demo != "" {
		// The demo keeps out of ~/.stratum and the keychain: a fresh data
		// directory and one scripted account.
		dir, err := os.MkdirTemp("", "stratum-demo-")
		if err != nil {
			log.Fatal(err)
		}
		defer os.RemoveAll(dir)
		auth.Dir, opts.Creds, opts.Demo = dir, &auth.Memory{}, true
	}
	if err := os.MkdirAll(auth.Dir, 0o700); err != nil {
		log.Fatal(err)
	}
	opts.DB = boot.DBPath()
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
	mygo.SetFrontend(web.Assets())
	mygo.BindAs("Pierre", a.PierreBridge())
	mygo.Use(native.Plugin) // checks a day apart; builds without updates never do
	mygo.App.WhenReady(func() {
		mygo.App.SetMenu(menu(a))
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:          "Stratum",
			Width:          1200,
			Height:         780,
			MinWidth:       760,
			MinHeight:      480,
			StateKey:       "main",
			TitleBarStyle:  mygo.TitleBarHiddenInset,
			TitleBarHeight: 52,
			Vibrancy:       mygo.VibrancyGlass,
			Content:        ui.View(a.View),
		})
		a.SetCloseWindow(win.Close)
		a.Serve(ctx, k.Server, win.Update)
		a.UseDesktop()
		if mygo.WebViewAvailable() {
			wv, err := win.NewWebView(mygo.WebViewOptions{
				URL:             "/",
				BackgroundColor: "light-dark(#fbfbfc, #0d0d0e)",
			})
			if err != nil {
				log.Printf("pierre webview unavailable, using native fallback: %v", err)
			} else {
				wv.Page().OnDidFailLoad(func(le *mygo.LoadError) {
					log.Printf("pierre webview load failed: %v", le)
				})
				a.SetWebView(wv)
			}
			// The panel's browser: someone else's pages, which call no Go,
			// and which the agent's browser tool drives as the user sees.
			// Their cookies and storage are a session of their own, apart
			// from the app's page, which Clear History… empties.
			browsing := mygo.NewSession("browser")
			a.UseBrowser(func() (*mygo.WebView, error) {
				return win.NewWebView(mygo.WebViewOptions{
					BackgroundColor: "light-dark(#ffffff, #0d0d0e)",
					Page: mygo.PageOptions{
						Session:         browsing,
						Untrusted:       true,
						PreloadScript:   preview.LogScript,
						SwipeNavigation: true,
						PinchZoom:       true,
						UserAgentSuffix: "Version/26.0 Safari/605.1.15",
					},
				})
			}, func() error { return browsing.ClearData(mygo.SiteDataAll) })
			preview.Default.SetDriver(a.BrowserDriver())
		}
		// Computer use asks for the permissions it lacks with the guide to
		// System Settings: the app to drag into its list.
		computer.SetPermissions(func() []string {
			var missing []string
			for _, p := range []permissionflow.Permission{permissionflow.Accessibility, permissionflow.ScreenRecording} {
				if !permissionflow.Allowed(p) {
					missing = append(missing, string(p))
				}
			}
			return missing
		})
		computer.SetGuide(func(ctx context.Context, missing []string) error {
			for _, p := range missing {
				if _, err := permissionflow.Guide(ctx, permissionflow.Permission(p), permissionflow.GuideOptions{}); err != nil {
					return err
				}
			}
			return nil
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// menu is the default menu bar with "Check for Updates…" after About and
// the app's own commands: Settings, a Session menu and the sidebar in View.
func menu(a *app.App) *mygo.Menu {
	session, settings, view, clearHistory := a.MenuBar()
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Stratum", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			updater.MenuItem(),
			mygo.Separator(),
			settings,
			clearHistory,
			mygo.Separator(),
			{Role: mygo.RoleServices},
			mygo.Separator(),
			{Role: mygo.RoleHide},
			{Role: mygo.RoleHideOthers},
			{Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Label: locales.MenuSession(), Submenu: session},
		{Role: mygo.RoleEditMenu},
		{Label: locales.MenuView(), Submenu: view},
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
