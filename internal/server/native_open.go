package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	"teamcross/internal/domain"
)

const nativeDesktopURLPrefix = "codex://threads/"

var (
	errNativeManagedSource = errors.New("与 managed Run 关联的 Session 暂不支持在原生界面打开")
	errNativeOpenBusy      = errors.New("本机存在切换中或身份/关闭状态未确认的 managed Run；暂不能安全打开原生会话")
)

type nativeOpenRequest struct {
	SnapshotID  string `json:"snapshotId"`
	ConfirmOpen bool   `json:"confirmOpen"`
}

func (app *App) handleOpenNativeSession(w http.ResponseWriter, r *http.Request) {
	threadID, ok := app.authorizeThread(w, r)
	if !ok {
		return
	}
	var input nativeOpenRequest
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.SnapshotID == "" || len(input.SnapshotID) > 128 || !input.ConfirmOpen {
		writeError(w, 422, "native_open_confirmation", "请选择快照并确认在 Codex Desktop 打开；此操作不代表恢复原 CLI。")
		return
	}
	stored, err := app.store.GetSessionSnapshot(r.Context(), threadID, input.SnapshotID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	if _, err := nativeDesktopURL(stored.Source); err != nil {
		writeError(w, 422, "native_open_source", err.Error())
		return
	}
	// Stored evidence identifies what to verify, never grants local authority.
	// No Run, resume, prompt, cwd override or Session enumeration is involved.
	fresh, err := app.readReviewSnapshot(r.Context(), sessionImportRequest{Provider: stored.Source.Provider, SessionID: stored.Source.SessionID})
	if err != nil || !sameFollowSource(stored.Source, fresh.Source) || !fresh.Capabilities.Read || !fresh.Capabilities.Open {
		writeError(w, 422, "native_open_unsupported", "当前来源或版本尚未通过 Codex Desktop 精确打开验证；未发送打开请求。")
		return
	}
	link, err := nativeDesktopURL(fresh.Source)
	if err != nil {
		writeError(w, 422, "native_open_source", err.Error())
		return
	}
	if err := app.requestNativeDesktopOpen(r.Context(), fresh.Source, link); err != nil {
		if errors.Is(err, errNativeManagedSource) || errors.Is(err, errNativeOpenBusy) {
			writeError(w, http.StatusConflict, "native_open_writer_blocked", err.Error())
		} else {
			writeError(w, http.StatusBadGateway, "native_open_failed", "系统未确认接收 Codex Desktop 打开请求；Team Cross 未创建 Run 或发送工作指令。")
		}
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status": "requested", "target": "codex-desktop", "provider": fresh.Source.Provider,
		"sessionId": fresh.Source.SessionID,
		"message":   "已将 Codex Desktop 打开请求交给系统；尚未验证原生界面显示结果。",
	})
}

func nativeDesktopURL(source domain.SessionRef) (string, error) {
	if source.Provider != "codex" || source.IdentityKind != "thread.id" {
		return "", fmt.Errorf("仅支持真实 Codex thread.id 的 Desktop 打开请求")
	}
	switch source.Surface {
	case "cli", "desktop", "app-server", "vscode":
	case "managed":
		return "", errNativeManagedSource
	default:
		return "", fmt.Errorf("原生 Session 来源界面尚未确认")
	}
	if !nativeConversationUUID(source.SessionID) {
		return "", fmt.Errorf("Codex thread.id 必须是完整 UUID，不能含路径或查询参数")
	}
	if id := source.NativeIDs["threadId"]; id != "" && id != source.SessionID {
		return "", fmt.Errorf("原生 Thread 身份与来源不一致")
	}
	return nativeDesktopURLPrefix + source.SessionID, nil
}

func nativeConversationUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return len(value) == 36 && err == nil && parsed != uuid.Nil && parsed.String() == strings.ToLower(value)
}

func (app *App) requestNativeDesktopOpen(ctx context.Context, source domain.SessionRef, link string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Existing transition admission and Run identity publication share app.mu.
	// Reject in-flight work, then hold it through the short OS request so a new
	// managed Writer cannot appear between this final check and opening Desktop.
	// Store methods below close their reads before returning; no Store transaction
	// waits for app.mu while retaining a database lock.
	app.mu.Lock()
	defer app.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, switching := range app.switching {
		if switching {
			return errNativeOpenBusy
		}
	}
	for _, count := range app.agentOps {
		if count > 0 {
			return errNativeOpenBusy
		}
	}
	// Include all Team Cross Threads, including historical Runs not reloaded into
	// memory on restart. Never enumerate the Provider's personal session history.
	threads, err := app.store.ListThreads(ctx)
	if err != nil {
		return err
	}
	confirmedClosures := map[string]bool{}
	for _, thread := range threads {
		runs, err := app.store.ListAgentRuns(ctx, thread.ID)
		if err != nil {
			return err
		}
		for _, run := range runs {
			if run.Provider != source.Provider {
				continue
			}
			// V1 deliberately forbids every previously managed target, even after
			// confirmed close. Open is not a native Writer handback mechanism.
			if strings.EqualFold(run.SessionID, source.SessionID) {
				return errNativeManagedSource
			}
			confirmedClosed := run.Status == "closed" && !run.ClosedAt.IsZero()
			confirmedClosures[run.ID] = confirmedClosed
			if !confirmedClosed && (!nativeConversationUUID(run.SessionID) || run.Status == "identity_conflict" || run.Status == "archived") {
				return errNativeOpenBusy
			}
		}
	}
	checkMemory := func(run *managedRun) error {
		if run == nil || run.Provider != source.Provider {
			return nil
		}
		if strings.EqualFold(run.SessionID, source.SessionID) {
			return errNativeManagedSource
		}
		if !confirmedClosures[run.ID] && (run.IdentityConflict || !nativeConversationUUID(run.SessionID) || run.Status == "archived") {
			return errNativeOpenBusy
		}
		return nil
	}
	for _, run := range app.runsByID {
		if err := checkMemory(run); err != nil {
			return err
		}
	}
	for _, run := range app.runs {
		if err := checkMemory(run); err != nil {
			return err
		}
	}
	opener := app.nativeOpener
	if opener == nil {
		opener = openNativeDesktop
	}
	return opener(ctx, link)
}

func nativeDesktopOpenCommand(ctx context.Context, link string) (*exec.Cmd, error) {
	if !strings.HasPrefix(link, nativeDesktopURLPrefix) || !nativeConversationUUID(strings.TrimPrefix(link, nativeDesktopURLPrefix)) {
		return nil, fmt.Errorf("invalid minimal Codex Desktop link")
	}
	return exec.CommandContext(ctx, "/usr/bin/open", "-b", "com.openai.codex", link), nil
}

func openNativeDesktop(ctx context.Context, link string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("Codex Desktop opening is only supported on macOS")
	}
	command, err := nativeDesktopOpenCommand(ctx, link)
	if err != nil {
		return err
	}
	return command.Run()
}
