package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"teamcross/apps/desktop/internal/appinstance"
	"teamcross/apps/desktop/internal/coreclient"
	"teamcross/apps/desktop/internal/desktopactions"
	"teamcross/apps/desktop/internal/desktopserver"
	"teamcross/apps/desktop/internal/launchqueue"
	"teamcross/apps/desktop/internal/native"
	"teamcross/apps/desktop/internal/previewassets"
	"teamcross/internal/service"
	"teamcross/internal/webassets"
)

func main() {
	data := flag.String("data-dir", os.Getenv("TEAMCROSS_DATA_DIR"), "Isolated Core directory (required; also TEAMCROSS_DATA_DIR)")
	probe := flag.Bool("probe", false, "Show the transport diagnostic page instead of the main UI")
	connectOnly := flag.Bool("connect-only", false, "Connect to an existing test Core without starting one")
	flag.Parse()
	if *data == "" {
		log.Fatal("Desktop Preview requires an isolated --data-dir")
	}
	directory, err := service.Normalize(*data)
	if err != nil {
		log.Fatal("Cannot resolve the test directory")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal("Cannot resolve the home directory")
	}
	production, err := service.Normalize(filepath.Join(home, "Library", "Application Support", "Team Cross Next"))
	if err != nil || directory == production {
		log.Fatal("Desktop Preview cannot use the installed Core directory")
	}
	instance, err := appinstance.New(directory)
	if err != nil {
		log.Fatal("Cannot open desktop instance lock")
	}
	defer instance.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inbox := &launchqueue.Queue{}
	defer inbox.Close()
	inbox.Add(appinstance.NewRequest(nil), time.Now())
	var primary atomic.Bool
	client, err := coreclient.New(directory)
	if err != nil {
		log.Fatal("Cannot initialise the Core client")
	}
	var files fs.FS
	files, err = fs.Sub(webassets.Dist, "dist")
	if *probe {
		files = previewassets.FS
	}
	if err != nil {
		log.Fatal("Cannot load desktop resources")
	}
	var app *application.App
	actions := desktopactions.New(func(text string) bool {
		var copied bool
		application.InvokeSync(func() { copied = app.Clipboard.SetText(text) })
		return copied
	})
	scope := sha256.Sum256([]byte(directory))
	assets, err := desktopserver.New(files, client, actions, hex.EncodeToString(scope[:]))
	if err != nil {
		log.Fatal("Cannot load desktop resources")
	}
	app = application.New(application.Options{
		Name:        "Team Cross Desktop Preview",
		Description: "Team Cross desktop preview",
		Assets:      application.AssetOptions{Handler: assets, DisableLogging: true},
	})
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "main", Title: "Team Cross Desktop Preview", URL: "/",
		Width: 1400, Height: 900, MinWidth: 960, MinHeight: 640, Hidden: true,
	})
	show := func() { window.Show(); window.Focus() }
	message := func(text string) {
		dialog := app.Dialog.Error().SetTitle("Team Cross").SetMessage(text)
		if primary.Load() {
			window.Show()
			dialog.AttachToWindow(window)
		}
		dialog.Show()
	}
	app.Event.OnApplicationEvent(events.Common.ApplicationLaunchedWithUrl, func(event *application.ApplicationEvent) {
		invitation, ok := launchqueue.InvitationURL(event.Context().URL())
		if !ok {
			return
		}
		if !inbox.Add(appinstance.NewRequest([]string{invitation}), time.Now()) {
			message("待确认邀请过多，请先处理已打开的邀请。 / Too many pending invitations. Handle the existing invitations first.")
		}
	})
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		if !native.Configure(window) {
			log.Print("Cannot configure the desktop window")
			app.Quit()
			return
		}
		go func() {
			// LaunchServices delivers cold-launch URL events after finishLaunching.
			// Keep the App event loop alive while claiming/forwarding, including
			// second copies; the main window remains hidden until ownership.
			time.Sleep(300 * time.Millisecond)
			deadline := time.Now().Add(15 * time.Second)
			for !primary.Load() {
				owner, err := instance.Claim(func(request appinstance.Request) bool { return inbox.Add(request, time.Now()) })
				if err != nil {
					message("无法建立桌面通信入口。 / Cannot establish desktop communication.")
					app.Quit()
					return
				}
				if owner {
					primary.Store(true)
					break
				}
				if request, ok := inbox.Front(time.Now()); ok && instance.Forward(request) {
					inbox.Done(request.ID)
				}
				if _, ok := inbox.Front(time.Now()); !ok {
					time.Sleep(300 * time.Millisecond)
					if _, ok := inbox.Front(time.Now()); !ok {
						app.Quit()
						return
					}
				}
				if time.Now().After(deadline) {
					message("已有桌面未响应，请在其恢复后重新打开邀请。 / The existing desktop is not responding. Reopen the invitation after it recovers.")
					app.Quit()
					return
				}
				time.Sleep(150 * time.Millisecond)
			}
			if !*connectOnly {
				executable, err := os.Executable()
				if err == nil {
					helper := filepath.Join(filepath.Dir(executable), "..", "Resources", "teamcross")
					_, err = service.Ensure(ctx, directory, helper, nil)
				}
				if err != nil {
					message("本机服务启动失败，请检查设置。 / Core could not start. Check the settings.")
				}
			}
			for ctx.Err() == nil {
				request, ok := inbox.Front(time.Now())
				if !ok {
					time.Sleep(100 * time.Millisecond)
					continue
				}
				if len(request.URLs) == 0 {
					show()
				}
				for _, invitation := range request.URLs {
					if !inbox.Live(request.ID, time.Now()) {
						message("邀请转交已过期，请重新打开链接。 / Invitation delivery expired. Reopen the link.")
						break
					}
					stage, done := context.WithTimeout(ctx, 5*time.Second)
					id, err := client.StageInvitation(stage, invitation)
					done()
					if err != nil {
						message("无法打开邀请。请确认本机服务已连接，再重新打开链接。 / Cannot open the invitation. Check the Core connection, then reopen the link.")
						continue
					}
					confirmation := app.Window.NewWithOptions(application.WebviewWindowOptions{
						Name: "invitation-" + id, Title: "Team Cross · 加入协作 / Join", URL: "/#/join/" + id,
						Width: 1100, Height: 800, MinWidth: 960, MinHeight: 640, Hidden: true,
					})
					if !native.Configure(confirmation) {
						confirmation.Close()
						continue
					}
					confirmation.Show()
					confirmation.Focus()
				}
				inbox.Done(request.ID)
			}
		}()
	})
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		event.Cancel()
		window.Hide()
	})
	app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
		if primary.Load() {
			show()
		}
	})
	menu := app.Menu.New()
	menu.AddRole(application.AppMenu)
	menu.AddRole(application.EditMenu)
	view := menu.AddSubmenu("窗口 / Window")
	view.Add("显示窗口 / Show Window").SetAccelerator("CmdOrCtrl+1").OnClick(func(*application.Context) {
		if primary.Load() {
			show()
		}
	})
	menu.AddRole(application.WindowMenu)
	app.Menu.Set(menu)
	if err := app.Run(); err != nil {
		log.Fatal("Desktop Preview could not start")
	}
}
