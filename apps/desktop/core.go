package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"teamcross/apps/desktop/internal/native"
	"teamcross/internal/service"
	"teamcross/internal/uilanguage"
)

// ensureCore is an explicit native action. Page retries remain read-only and
// never spawn a service or replay a failed business request.
func (s *desktopShell) ensureCore() {
	if s.busy() || !s.operationBusy.CompareAndSwap(false, true) {
		return
	}
	defer s.operationBusy.Store(false)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	executable, err := os.Executable()
	if err == nil {
		_, err = service.Ensure(ctx, s.directory, filepath.Join(filepath.Dir(executable), "..", "Resources", "teamcross"), nil)
	}
	if err == nil {
		// The desktop's compatibility check is deliberately stricter than the
		// common CLI protocol probe. An existing incompatible Core stays intact.
		_, err = s.client.Status(ctx)
	}
	s.refresh(ctx)
	application.InvokeSync(func() { s.showMain("") })
	if err == nil {
		native.Refresh(s.main)
		native.Refresh(s.quick)
		return
	}
	message, button := "The local service could not be connected. Check Diagnostics and Settings, then try again. An existing service is not replaced automatically.", "OK"
	if localLanguage(s.directory) == uilanguage.Chinese {
		message, button = "本机服务尚未连接。请查看诊断与设置后重试；已有服务不会被自动替换。", "知道了"
	}
	closed := make(chan struct{})
	dialog := s.app.Dialog.Error().SetTitle("Team Cross").SetMessage(message).AttachToWindow(s.main)
	dialog.AddButton(button).SetAsCancel().SetAsDefault().OnClick(func() { close(closed) })
	dialog.Show()
	<-closed
}
