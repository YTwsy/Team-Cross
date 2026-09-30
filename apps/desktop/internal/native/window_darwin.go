// Package native preserves WKWebView navigation and dialog boundaries.
package native

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#include <stdbool.h>
#include <stdlib.h>
bool configureTeamCrossWindow(void *window);
void *teamCrossTrayIcon(int *length);
void teamCrossWindowScript(void *window, const char *script);
void presentTeamCrossWindow(void *window);
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"github.com/wailsapp/wails/v3/pkg/application"
	"unsafe"
)

// The shared React bundle does not load Wails' JavaScript runtime. Deliver only
// host-owned UI notifications through the already guarded WKWebView, without
// exposing a generic native binding or waiting for Wails' runtime-ready signal.
func windowScript(window *application.WebviewWindow, script string) {
	text := C.CString(script)
	defer C.free(unsafe.Pointer(text))
	application.InvokeSync(func() { C.teamCrossWindowScript(window.NativeWindow(), text) })
}

func Navigate(window *application.WebviewWindow, route string) {
	encoded, _ := json.Marshal(route)
	windowScript(window, "location.hash="+string(encoded))
}

func SetPinned(window *application.WebviewWindow, pinned bool) {
	windowScript(window, fmt.Sprintf("window.dispatchEvent(new CustomEvent('teamcross-pinned',{detail:%t}))", pinned))
}

func Refresh(window *application.WebviewWindow) {
	windowScript(window, "window.dispatchEvent(new Event('focus'))")
}

// Present is only used by explicit open/reopen actions, never background reads.
func Present(window *application.WebviewWindow) {
	window.Show()
	application.InvokeSync(func() { C.presentTeamCrossWindow(window.NativeWindow()) })
}

func Configure(window *application.WebviewWindow) bool {
	var ok bool
	application.InvokeSync(func() { ok = bool(C.configureTeamCrossWindow(window.NativeWindow())) })
	return ok
}

func TrayIcon() []byte {
	var icon []byte
	application.InvokeSync(func() {
		var length C.int
		data := C.teamCrossTrayIcon(&length)
		if data != nil {
			defer C.free(data)
			icon = C.GoBytes(data, length)
		}
	})
	return icon
}
