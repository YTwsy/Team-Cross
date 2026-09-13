package collab

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"

	"teamcross/internal/nativecodex"
	"teamcross/internal/problem"
)

// PersonalDesktopPlan opens the persisted fork in the owner's ordinary Desktop.
// It does not attach a direct client, resume the runtime, or change input ownership.
func (a *App) PersonalDesktopPlan(ctx context.Context, id string, launch bool) (map[string]any, error) {
	s, err := a.owned(id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	r := s.record
	s.mu.Unlock()
	provider, err := providerName(r.Provider)
	if err != nil || provider != "codex" {
		return nil, fmt.Errorf("只有本机发起的 Codex 协作可以在个人 Codex 中打开")
	}
	if r.SessionID == "" || r.SessionID == r.SourceID {
		return nil, fmt.Errorf("协作会话尚未创建，请等待创建完成后重试")
	}
	app := a.desktop()
	if stat, err := os.Stat(app); err != nil || !stat.IsDir() {
		return nil, problem.New("client_missing", "未找到 Codex Desktop", "请在设置中选择已安装的应用")
	}
	link := (&url.URL{Scheme: "codex", Host: "threads", Path: "/" + r.SessionID}).String()
	command := "open -a " + nativecodex.Quote(app) + " " + nativecodex.Quote(link)
	note := "在个人 Codex 中定位此协作会话。"
	if launch {
		if out, err := exec.CommandContext(ctx, "open", "-a", app, link).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("无法打开个人 Codex：%v %s；请检查 Desktop 安装后重试", err, out)
		}
		note = "已请求个人 Codex 打开此协作会话，请在 Desktop 中查看。"
	}
	return map[string]any{
		"sessionId": r.SessionID,
		"url":       link,
		"command":   command,
		"launched":  launch,
		"note":      note,
	}, nil
}
