package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"teamcross/apps/desktop/internal/coreclient"
	"teamcross/apps/desktop/internal/desktopserver"
	"teamcross/apps/desktop/internal/previewassets"
	"teamcross/internal/service"
)

func main() {
	data := flag.String("data-dir", "", "Existing isolated Core discovery directory (required)")
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
	client, err := coreclient.New(directory)
	if err != nil {
		log.Fatal("Cannot initialise the Core client")
	}
	assets, err := desktopserver.New(previewassets.FS, client)
	if err != nil {
		log.Fatal("Cannot load desktop resources")
	}
	app := application.New(application.Options{
		Name:        "Team Cross Desktop Preview",
		Description: "Team Cross isolated desktop transport preview",
		Assets:      application.AssetOptions{Handler: assets, DisableLogging: true},
	})
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "main", Title: "Team Cross Desktop Preview", URL: "/",
		Width: 1024, Height: 780, MinWidth: 640, MinHeight: 540,
	})
	show := func() { window.Show(); window.Focus() }
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
