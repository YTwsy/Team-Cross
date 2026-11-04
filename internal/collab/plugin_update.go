package collab

import (
	"crypto/subtle"
	"net/http"
	"teamcross/internal/mcpassets"
)

func (a *App) pluginUpdateHTTP(w http.ResponseWriter, r *http.Request, path string) bool {
	if path != "plugin/panel-version" && path != "plugin/panel-bundle" {
		return false
	}
	if r.Method != "GET" || r.Header.Get("Origin") != "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+a.Token)) != 1 {
		http.Error(w, "插件资源访问不可用", http.StatusForbidden)
		return true
	}
	bundle := mcpassets.Current
	if path == "plugin/panel-version" {
		bundle.Script, bundle.Style = "", ""
	} else if r.URL.Query().Get("revision") != bundle.Revision {
		http.Error(w, "插件资源版本已变化", http.StatusConflict)
		return true
	}
	respond(w, bundle, nil)
	return true
}
