package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
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
	"teamcross/apps/desktop/internal/lifecycle"
	"teamcross/apps/desktop/internal/native"
	"teamcross/apps/desktop/internal/previewassets"
	"teamcross/internal/service"
	"teamcross/internal/uilanguage"
	"teamcross/internal/webassets"
)

func main() {
	data := flag.String("data-dir", os.Getenv("TEAMCROSS_DATA_DIR"), "Isolated Core directory (required; also TEAMCROSS_DATA_DIR)")
	probe := flag.Bool("probe", false, "Show the transport diagnostic page instead of the main UI")
	connectOnly := flag.Bool("connect-only", false, "Connect to an existing test Core without starting one")
	leaveCore := flag.Bool("leave-core-running", false, "Diagnostic fixture mode: quit only the shell")
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
	quit := &lifecycle.Quit{}
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
	var shell atomic.Pointer[desktopShell]
	var allowQuit atomic.Bool
	startup := &lifecycle.Startup{Quit: func() {
		if primary.Load() {
			quit.Request()
		} else {
			allowQuit.Store(true)
			app.Quit()
		}
	}}
	enqueue := func(action, route string) bool {
		host := shell.Load()
		return host != nil && host.Enqueue(action, route)
	}
	actions := desktopactions.New(desktopactions.Actions{Clipboard: func(text string) bool {
		var copied bool
		application.InvokeSync(func() { copied = app.Clipboard.SetText(text) })
		return copied
	}, Open: func(route string) bool { return enqueue("open", route) },
		Pin:  func() bool { return enqueue("pin", "") },
		Menu: func() bool { return enqueue("menu", "") },
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
		ShouldQuit: func() bool {
			if allowQuit.Load() || *leaveCore {
				return true
			}
			if host := shell.Load(); host != nil && host.languageBusy.Load() {
				return false
			}
			startup.RequestQuit()
			return false
		},
	})
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "main", Title: "Team Cross Desktop Preview", URL: "/",
		Width: 1400, Height: 900, MinWidth: 960, MinHeight: 640, Hidden: true,
	})
	quick := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "quick", Title: "Team Cross · Quick View", URL: "/#/library/quick",
		Width: 470, Height: 650, MinWidth: 420, MinHeight: 440, Hidden: true, HideOnEscape: true,
	})
	show := func() {
		application.InvokeSync(func() {
			if host := shell.Load(); host != nil {
				host.showMain("")
			}
		})
	}
	quit.Core = client
	quit.Confirm = func(status service.Status) bool {
		show()
		choice := make(chan bool, 1)
		zh := status.ResolvedLanguage == uilanguage.Chinese
		title, message := "Stop Core and quit?", fmt.Sprintf("This disconnects %d active collaborations on this Mac. Hosted runtimes stop; sessions, code and working directories are preserved.", status.Active)
		stop, cancel := "Stop and Quit", "Cancel"
		if zh {
			title = "停止本机服务并退出？"
			message = fmt.Sprintf("将断开本机的 %d 个活动协作。发起的运行时会停止，参与的协作只断开本机连接。会话、代码和工作目录都会保留。", status.Active)
			stop, cancel = "停止并退出", "取消"
		}
		dialog := app.Dialog.Question().SetTitle(title).SetMessage(message).AttachToWindow(window)
		dialog.AddButton(stop).OnClick(func() { choice <- true })
		dialog.AddButton(cancel).SetAsCancel().SetAsDefault().OnClick(func() { choice <- false })
		dialog.Show()
		return <-choice
	}
	quit.Failed = func(error) {
		show()
		message := "Core could not confirm shutdown. Check its status and try again. Your window remains open."
		button := "OK"
		if localLanguage(directory) == uilanguage.Chinese {
			message = "本机服务尚未确认停止。请检查服务状态后重试；窗口和未提交内容继续保留。"
			button = "知道了"
		}
		closed := make(chan struct{})
		dialog := app.Dialog.Error().SetTitle("Team Cross").SetMessage(message).AttachToWindow(window)
		dialog.AddButton(button).SetAsDefault().OnClick(func() { close(closed) })
		dialog.Show()
		<-closed
	}
	quit.Finished = func() { allowQuit.Store(true); app.Quit() }
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
		if startup.Quitting() || quit.Busy() {
			message("正在退出，请取消退出或稍后重新打开邀请。 / Quitting. Cancel quit or reopen the invitation later.")
			return
		}
		if !inbox.Add(appinstance.NewRequest([]string{invitation}), time.Now()) {
			message("待确认邀请过多，请先处理已打开的邀请。 / Too many pending invitations. Handle the existing invitations first.")
		}
	})
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		if !native.Configure(window) || !native.Configure(quick) {
			log.Print("Cannot configure the desktop window")
			startup.Finish()
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
				if startup.Quitting() {
					startup.Finish()
					return
				}
				owner, err := instance.Claim(func(request appinstance.Request) bool {
					return !startup.Quitting() && !quit.Busy() && inbox.Add(request, time.Now())
				})
				if err != nil {
					message("无法建立桌面通信入口。 / Cannot establish desktop communication.")
					startup.Finish()
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
						startup.Finish()
						app.Quit()
						return
					}
				}
				if time.Now().After(deadline) {
					message("已有桌面未响应，请在其恢复后重新打开邀请。 / The existing desktop is not responding. Reopen the invitation after it recovers.")
					startup.Finish()
					app.Quit()
					return
				}
				time.Sleep(150 * time.Millisecond)
			}
			if !*connectOnly && !startup.Quitting() {
				executable, err := os.Executable()
				if err == nil {
					helper := filepath.Join(filepath.Dir(executable), "..", "Resources", "teamcross")
					_, err = service.Ensure(ctx, directory, helper, nil)
				}
				if err != nil && !startup.Quitting() {
					message("本机服务启动失败，请检查设置。 / Core could not start. Check the settings.")
				}
			}
			// Install menus and global shortcuts only in the owning shell. Publish
			// it atomically because native HTTP actions already have an event loop.
			application.InvokeSync(func() {
				host := newDesktopShell(app, window, quick, client, directory, quit.Busy)
				host.Start(ctx)
				shell.Store(host)
			})
			startup.Finish()
			for ctx.Err() == nil {
				if quit.Busy() {
					time.Sleep(100 * time.Millisecond)
					continue
				}
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
					native.SetAppearance(confirmation, localPreferences(directory).UITheme)
					native.Present(confirmation)
				}
				inbox.Done(request.ID)
			}
		}()
	})
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		event.Cancel()
		application.InvokeSync(func() { window.Hide() })
	})
	app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
		if primary.Load() {
			show()
		}
	})
	if err := app.Run(); err != nil {
		log.Fatal("Desktop Preview could not start")
	}
}
