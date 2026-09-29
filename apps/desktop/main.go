package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"teamcross/apps/desktop/internal/appinstance"
	"teamcross/apps/desktop/internal/coreclient"
	"teamcross/apps/desktop/internal/desktopactions"
	"teamcross/apps/desktop/internal/desktopserver"
	"teamcross/apps/desktop/internal/native"
	"teamcross/apps/desktop/internal/previewassets"
	"teamcross/internal/service"
	"teamcross/internal/webassets"
)

func main() {
	data := flag.String("data-dir", "", "Existing isolated Core discovery directory (required)")
	probe := flag.Bool("probe", false, "Show the transport diagnostic page instead of the main UI")
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
	showRequests := make(chan struct{}, 32)
	receive := func(request appinstance.Request) bool {
		// Invitation delivery is introduced separately from the shell protocol.
		if len(request.URLs) != 0 {
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
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		if !native.Configure(window) {
			log.Print("Cannot configure the desktop window")
			app.Quit()
			return
		}
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
