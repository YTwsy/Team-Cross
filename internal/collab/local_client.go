package collab

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"teamcross/internal/nativecodex"
)

// localClient handles Desktop account operations on the participant's computer.
// It never forwards credentials, login requests, or local preferences to A.
// Account changes use the dedicated client home, leaving ordinary Codex intact.
type localClient struct {
	app            *App
	id             string
	process        Runtime
	accountChanged bool
	emit           func(nativecodex.Message)
}

func localClientMethod(method string) bool {
	switch method {
	case "getAuthStatus", "account/read", "account/rateLimits/read",
		"account/login/start", "account/login/cancel", "account/logout",
		"config/batchWrite", "config/value/write", "experimentalFeature/enablement/set":
		return true
	}
	return false
}

// Native hook review updates the trust state of A's hooks. Keep this narrow:
// all other preference/configuration writes still belong to the local client.
// The host separately checks trusted mode and current input ownership.
func hookTrustWrite(method string, params map[string]any) bool {
	stateKey := func(edit map[string]any) bool {
		key, _ := edit["keyPath"].(string)
		return key == "hooks.state" || strings.HasPrefix(key, "hooks.state.")
	}
	if method == "config/value/write" {
		return stateKey(params)
	}
	if method != "config/batchWrite" {
		return false
	}
	edits, ok := params["edits"].([]any)
	if !ok || len(edits) == 0 {
		return false
	}
	for _, value := range edits {
		edit, ok := value.(map[string]any)
		if !ok || !stateKey(edit) {
			return false
		}
	}
	return true
}

func localClientRequest(method string, params map[string]any) bool {
	return localClientMethod(method) && !hookTrustWrite(method, params)
}

func localAccountNotification(method string) bool {
	return strings.HasPrefix(method, "account/") || method == "loginChatGptComplete" || method == "authStatusChange"
}

func (c *localClient) call(ctx context.Context, method string, params map[string]any, out any) error {
	readAccount := method == "getAuthStatus" || method == "account/read" || method == "account/rateLimits/read"
	// A locally signed-in participant can bootstrap Desktop without copying an
	// auth file. The token stays on this loopback connection. Subsequent login or
	// logout explicitly switches this instance to its isolated account runtime.
	if !c.accountChanged && readAccount {
		return c.app.readerCall(ctx, method, params, out)
	}
	if c.process == nil {
		home := filepath.Join(c.app.Config.DataDir, "clients", c.id, "desktop", "codex-home")
		if _, err := os.Stat(filepath.Join(home, "config.toml")); err != nil {
			if err = nativecodex.WriteConfig(home); err != nil {
				return err
			}
		}
		p, err := c.app.startProcess(ctx, home, c.app.Config.Repo, filepath.Join(home, "local-client.log"))
		if err != nil {
			return err
		}
		p.SetHandler(func(m nativecodex.Message) {
			if !localAccountNotification(m.Method) {
				return
			}
			if len(m.ID) > 0 {
				m.ID, _ = json.Marshal("teamcross-local:" + base64.RawURLEncoding.EncodeToString(m.ID))
			}
			c.emit(m)
		})
		c.process = p
	}
	if method == "account/login/start" || method == "account/logout" {
		c.accountChanged = true
	}
	if method == "config/batchWrite" || method == "config/value/write" {
		// The client cannot select A's config path (or an arbitrary local file).
		delete(params, "filePath")
		delete(params, "expectedVersion")
	}
	return c.process.Call(ctx, method, params, out)
}

func (c *localClient) reply(ctx context.Context, m nativecodex.Message) bool {
	var id string
	if json.Unmarshal(m.ID, &id) != nil || !strings.HasPrefix(id, "teamcross-local:") {
		return false
	}
	if c.process != nil {
		original, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, "teamcross-local:"))
		if err == nil {
			var result any
			_ = json.Unmarshal(m.Result, &result)
			_ = c.process.Reply(ctx, original, result)
		}
	}
	return true
}

func (c *localClient) close() {
	if c.process != nil {
		c.process.Close()
	}
}
