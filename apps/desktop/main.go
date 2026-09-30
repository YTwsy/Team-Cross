package main

import (
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
	"teamcross/apps/desktop/internal/lifecycle"
	"teamcross/apps/desktop/internal/native"
	"teamcross/apps/desktop/internal/previewassets"
	"teamcross/internal/service"
	"teamcross/internal/uilanguage"
	"teamcross/internal/webassets"
)

func main() {
	data := flag.String("data-dir", os.Getenv("TEAMCROSS_DATA_DIR"), "Isolated Core discovery directory (required)")
	probe := flag.Bool("probe", false, "Show the transport diagnostic page instead of the main UI")
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
	showRequests := make(chan struct{}, 32)
	receive := func(request appinstance.Request) bool {
		// Invitation delivery is introduced separately from the shell protocol.
		if quit.Busy() || len(request.URLs) != 0 {
			return false
		}
		select {
		case showRequests <- struct{}{}:
			return true
		default:
			return false
		}
	}
	request := appinstance.NewRequest(nil)
	deadline := time.Now().Add(15 * time.Second)
	for {
		primary, err := instance.Claim(receive)
		if err != nil {
			log.Fatal("Cannot claim desktop instance")
		}
		if primary {
			break
		}
		if instance.Forward(request) {
			return
		}
		if time.Now().After(deadline) {
			log.Fatal("The existing desktop is not responding; reopen it after it recovers")
		}
		time.Sleep(150 * time.Millisecond)
	}
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
	var ready, allowQuit atomic.Bool
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
		ShouldQuit: func() bool {
			if !ready.Load() || allowQuit.Load() || *leaveCore {
				return true
			}
			quit.Request()
			return false
		},
	})
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "main", Title: "Team Cross Desktop Preview", URL: "/",
		Width: 1400, Height: 900, MinWidth: 960, MinHeight: 640, Hidden: true,
	})
	show := func() { window.Show(); window.Focus() }
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
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		if !native.Configure(window) {
			log.Print("Cannot configure the desktop window")
			app.Quit()
			return
		}
		ready.Store(true)
		show()
		go func() {
			for range showRequests {
				show()
			}
		}()
	})
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		event.Cancel()
		window.Hide()
	})
	app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) { show() })
	menu := app.Menu.New()
	menu.AddRole(application.AppMenu)
	menu.AddRole(application.EditMenu)
	view := menu.AddSubmenu("窗口 / Window")
	view.Add("显示窗口 / Show Window").SetAccelerator("CmdOrCtrl+1").OnClick(func(*application.Context) { show() })
	menu.AddRole(application.WindowMenu)
	app.Menu.Set(menu)
	if err := app.Run(); err != nil {
		log.Fatal("Desktop Preview could not start")
	}
}
