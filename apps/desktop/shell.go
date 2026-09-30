package main

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"teamcross/apps/desktop/internal/coreclient"
	"teamcross/apps/desktop/internal/desktopactions"
	"teamcross/apps/desktop/internal/native"
	"teamcross/internal/service"
	"teamcross/internal/uilanguage"
)

type windowCommand struct{ action, route string }
type menuLabel struct {
	item   *application.MenuItem
	zh, en string
}

// Window and menu state is confined to the AppKit thread. Native HTTP handlers
// only enqueue bounded, fixed UI actions and never wait for a menu to close.
type desktopShell struct {
	app                *application.App
	main, quick        *application.WebviewWindow
	tray               *application.SystemTray
	client             *coreclient.Client
	directory          string
	busy               func() bool
	ready              atomic.Bool
	languageBusy       atomic.Bool
	commands           chan windowCommand
	pinned             bool
	menuOpen           bool
	labels             []menuLabel
	status, quickEntry *application.MenuItem
	languages          map[string]*application.MenuItem
}

func newDesktopShell(app *application.App, main, quick *application.WebviewWindow, client *coreclient.Client, directory string, busy func() bool) *desktopShell {
	s := &desktopShell{app: app, main: main, quick: quick, client: client, directory: directory, busy: busy, commands: make(chan windowCommand, 16), languages: make(map[string]*application.MenuItem)}
	menu := app.Menu.New()
	menu.AddRole(application.AppMenu)
	menu.AddRole(application.EditMenu)
	menu.AddRole(application.WindowMenu)
	view := menu.FindByRole(application.WindowMenu).GetSubmenu()
	view.AddSeparator()
	s.add(view, "显示主窗口", "Show Main Window", func() { s.showMain("") }).SetAccelerator("CmdOrCtrl+1")
	s.add(view, "协作速览", "Quick View", s.toggleQuick).SetAccelerator("CmdOrCtrl+2")
	if item := menu.FindByRole(application.Quit); item != nil {
		s.labels = append(s.labels, menuLabel{item, "退出 Team Cross", "Quit Team Cross"})
	}
	app.Menu.Set(menu)

	serviceMenu := app.Menu.New()
	serviceMenu.Add("Team Cross").SetEnabled(false)
	s.status = serviceMenu.Add("").SetEnabled(false)
	serviceMenu.AddSeparator()
	s.quickEntry = s.add(serviceMenu, "协作速览", "Quick View", s.toggleQuick)
	s.add(serviceMenu, "打开协作空间", "Open Spaces", func() { s.showMain("/") })
	s.add(serviceMenu, "打开资源库", "Open Library", func() { s.showMain("/library") })
	s.add(serviceMenu, "加入协作…", "Join a Collaboration…", func() { s.showMain("/join") })
	s.add(serviceMenu, "诊断与设置", "Diagnostics and Settings", func() { s.showMain("/settings") })
	language := serviceMenu.AddSubmenu("Language")
	s.labels = append(s.labels, menuLabel{serviceMenu.FindByLabel("Language"), "语言", "Language"})
	for _, choice := range []struct{ mode, zh, en string }{{"auto", "跟随系统", "Follow System"}, {"zh-CN", "简体中文", "Simplified Chinese"}, {"en", "English", "English"}} {
		item := language.AddCheckbox(choice.en, false)
		s.labels = append(s.labels, menuLabel{item, choice.zh, choice.en})
		s.languages[choice.mode] = item
		item.OnClick(func(*application.Context) { s.setLanguage(choice.mode) })
	}
	serviceMenu.AddSeparator()
	s.add(serviceMenu, "退出 Team Cross", "Quit Team Cross", app.Quit)
	app.ContextMenu.Add("services", &application.ContextMenu{Menu: serviceMenu})
	s.tray = app.SystemTray.New()
	s.tray.SetLabel("TC")
	s.tray.SetTooltip("Team Cross")
	s.tray.OnClick(func() { s.Enqueue("toggle", "") })
	s.tray.OnRightClick(func() { s.Enqueue("menu", "") })
	quick.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) { event.Cancel(); application.InvokeSync(func() { quick.Hide() }) })
	quick.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) {
		application.InvokeSync(func() {
			if !s.pinned && !s.menuOpen {
				quick.Hide()
			}
		})
	})
	return s
}

func (s *desktopShell) add(menu *application.Menu, zh, en string, action func()) *application.MenuItem {
	item := menu.Add(en).OnClick(func(*application.Context) {
		if !s.busy() {
			application.InvokeSync(action)
		}
	})
	s.labels = append(s.labels, menuLabel{item, zh, en})
	return item
}

func (s *desktopShell) Enqueue(action, route string) bool {
	if !s.ready.Load() || s.busy() || s.languageBusy.Load() {
		return false
	}
	if action == "open" && !desktopactions.ValidRoute(route) {
		return false
	}
	select {
	case s.commands <- windowCommand{action, route}:
		return true
	default:
		return false
	}
}

func (s *desktopShell) Start(ctx context.Context) {
	if icon := native.TrayIcon(); len(icon) != 0 {
		s.tray.SetTemplateIcon(icon)
		s.tray.SetLabel("")
	}
	_ = s.app.GlobalShortcut.Register("Ctrl+Option+T", func() { s.Enqueue("toggle", "") })
	s.ready.Store(true)
	go func() {
		for {
			select {
			case <-ctx.Done():
				s.ready.Store(false)
				return
			case command := <-s.commands:
				application.InvokeSync(func() {
					if s.busy() {
						return
					}
					switch command.action {
					case "open":
						s.showMain(command.route)
					case "toggle":
						s.toggleQuick()
					case "pin":
						s.pinned = !s.pinned
						s.quick.SetAlwaysOnTop(s.pinned)
						native.SetPinned(s.quick, s.pinned)
					case "menu":
						if !s.quick.IsVisible() {
							s.toggleQuick()
						}
						s.menuOpen = true
						s.quick.OpenContextMenu(&application.ContextMenuData{Id: "services", X: 300, Y: 125})
						s.menuOpen = false
						if !s.pinned && !s.quick.IsFocused() {
							s.quick.Hide()
						}
					}
				})
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			s.refresh(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *desktopShell) showMain(route string) {
	if route != "" {
		native.Navigate(s.main, route)
	}
	if !s.pinned {
		s.quick.Hide()
	}
	s.main.Show()
	s.main.Focus()
}

func (s *desktopShell) toggleQuick() {
	if s.quick.IsVisible() {
		s.quick.Hide()
		return
	}
	if !s.pinned {
		_ = s.tray.PositionWindow(s.quick, 8)
		s.quick.SetAlwaysOnTop(false)
	}
	s.quick.Show()
	s.quick.Focus()
	native.Refresh(s.quick)
}

func (s *desktopShell) refresh(ctx context.Context) {
	probe, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	status, err := s.client.Status(probe)
	mode, resolved := localLanguagePreference(s.directory)
	if err == nil && status.Running {
		mode, resolved = status.UILanguage, status.ResolvedLanguage
	}
	application.InvokeSync(func() { s.updateMenu(status, err, mode, resolved) })
}

func (s *desktopShell) updateMenu(status service.Status, err error, mode, resolved string) {
	zh := resolved == uilanguage.Chinese
	for _, label := range s.labels {
		text := label.en
		if zh {
			text = label.zh
		}
		label.item.SetLabel(text)
	}
	if s.app.GlobalShortcut.IsRegistered("Ctrl+Option+T") {
		s.quickEntry.SetLabel(s.quickEntry.Label() + "    ⌃⌥T")
	}
	for choice, item := range s.languages {
		item.SetChecked(choice == mode)
		item.SetEnabled(!s.languageBusy.Load() && !s.busy())
	}
	text, title := "Core unavailable · Open settings", "Team Cross · Quick View"
	if zh {
		text, title = "服务暂不可用 · 请查看设置", "Team Cross · 协作速览"
	}
	if err == nil {
		if status.Running {
			text = fmt.Sprintf("Core running · Active collaborations: %d", status.Active)
			if zh {
				text = fmt.Sprintf("服务运行中 · 活动协作 %d", status.Active)
			}
		} else {
			text = "Core stopped"
			if zh {
				text = "服务已停止"
			}
		}
	}
	s.status.SetLabel(text)
	s.quick.SetTitle(title)
}

func (s *desktopShell) setLanguage(mode string) {
	if s.busy() || !s.languageBusy.CompareAndSwap(false, true) {
		return
	}
	defer s.languageBusy.Store(false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := s.client.SetLanguage(ctx, mode)
	s.refresh(ctx)
	if err != nil {
		application.InvokeSync(func() { s.showMain("") })
		message := "The language change was not confirmed. Check Core status before trying again."
		button := "OK"
		if localLanguage(s.directory) == uilanguage.Chinese {
			message = "语言更改尚未确认。请检查本机服务状态后重试。"
			button = "知道了"
		}
		closed := make(chan struct{})
		dialog := s.app.Dialog.Error().SetTitle("Team Cross").SetMessage(message).AttachToWindow(s.main)
		dialog.AddButton(button).SetAsDefault().OnClick(func() { close(closed) })
		dialog.Show()
		<-closed
	}
}
