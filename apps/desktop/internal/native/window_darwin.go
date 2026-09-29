// Package native preserves WKWebView navigation and dialog boundaries.
package native

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#include <stdbool.h>
bool configureTeamCrossWindow(void *window);
*/
import "C"

import "github.com/wailsapp/wails/v3/pkg/application"

func Configure(window *application.WebviewWindow) bool {
	var ok bool
	application.InvokeSync(func() { ok = bool(C.configureTeamCrossWindow(window.NativeWindow())) })
	return ok
}
