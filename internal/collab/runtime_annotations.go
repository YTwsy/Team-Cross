package collab

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"teamcross/internal/mcp"
	"teamcross/internal/runtimeconfig"
	"teamcross/internal/service"

	"github.com/google/uuid"
)

type runtimeAnnotationKey struct{}

type annotationLaunch struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

func (s *Session) annotationLaunch() (annotationLaunch, error) {
	executable := s.app.Config.Executable
	if executable == "" {
		var err error
		executable, err = os.Executable()
		if err != nil {
			return annotationLaunch{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.ExecutionRecord == nil {
		return annotationLaunch{}, fmt.Errorf("空间没有运行时")
	}
	if s.record.AnnotationToken == "" {
		s.record.AnnotationToken = uuid.NewString()
		if err := s.saveLocked(); err != nil {
			s.record.AnnotationToken = ""
			return annotationLaunch{}, err
		}
	}
	return annotationLaunch{Command: service.StableExecutable(executable), Args: []string{"mcp", "--data-dir", s.app.Config.DataDir, "--runtime-id", s.record.ID}, Env: map[string]string{mcp.RuntimeTokenEnv: s.record.AnnotationToken}}, nil
}

func (c annotationLaunch) codexOverrides(inherited []string) []string {
	return c.codexOverridesForMode(inherited, runtimeconfig.Restricted)
}
func (c annotationLaunch) codexOverridesForMode(inherited []string, mode runtimeconfig.Mode) []string {
	var entries []string
	if mode != runtimeconfig.Trusted {
		for _, name := range inherited {
			key, _ := json.Marshal(name)
			entries = append(entries, fmt.Sprintf("%s={enabled=false}", key))
		}
	}
	// Do not merge a user's same-named HTTP/STDIO transport or extra env into
	// our entry. Pick an unused name if the reserved base name already exists.
	name := mcp.RuntimeServer
	for suffix := 1; slices.Contains(inherited, name); suffix++ {
		name = fmt.Sprintf("%s_%d", mcp.RuntimeServer, suffix)
	}
	command, _ := json.Marshal(c.Command)
	args, _ := json.Marshal(c.Args)
	token, _ := json.Marshal(c.Env[mcp.RuntimeTokenEnv])
	// JSON strings and arrays of strings are also valid TOML values.
	entries = append(entries, fmt.Sprintf(`%s={command=%s,args=%s,env={%s=%s},enabled=true,required=true}`, name, command, args, mcp.RuntimeTokenEnv, token))
	// Put quoted server names inside a TOML table: Codex splits CLI key paths
	// on dots without parsing quotes, including dots within a server name.
	return []string{"mcp_servers={" + strings.Join(entries, ",") + "}"}
}

func (c annotationLaunch) claudeConfig() string {
	return c.claudeConfigNamed(mcp.RuntimeServer)
}
func (c annotationLaunch) claudeConfigNamed(name string) string {
	data, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{name: c}})
	return string(data)
}

func (a *App) runtimeAnnotationsHTTP(w http.ResponseWriter, r *http.Request, id string) {
	a.mu.Lock()
	s := a.sessions[id]
	a.mu.Unlock()
	if r.Method != "POST" || r.Header.Get("Origin") != "" || s == nil {
		http.Error(w, "批注工具访问不可用", http.StatusForbidden)
		return
	}
	s.mu.Lock()
	if s.record.ExecutionRecord == nil {
		s.mu.Unlock()
		http.Error(w, "空间没有运行时", 403)
		return
	}
	token := s.record.AnnotationToken
	valid := token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) == 1
	s.mu.Unlock()
	if !valid {
		http.Error(w, "批注工具凭据无效", http.StatusForbidden)
		return
	}
	var input struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if !decode(w, r, &input) {
		return
	}
	if err := mcp.ValidateRuntimeCall(input.Name, input.Arguments); err != nil {
		respond(w, nil, err)
		return
	}
	ctx := context.WithValue(r.Context(), runtimeAnnotationKey{}, token)
	if input.Name == "list_materials" {
		out, err := s.materialOperation(ctx, "materials", nil)
		respond(w, out, err)
		return
	}
	if input.Name == "read_material" {
		data, _ := json.Marshal(input.Arguments)
		var in MaterialRead
		if err := json.Unmarshal(data, &in); err != nil {
			respond(w, nil, err)
			return
		}
		out, err := s.readMaterial(ctx, in)
		respond(w, out, err)
		return
	}
	if input.Name == "read_annotations" {
		out, err := s.Context(ctx, "annotations", "", 0)
		if err == nil {
			if annotationID, _ := input.Arguments["annotationId"].(string); annotationID != "" {
				data := out.(map[string]any)
				found := []Annotation{}
				for _, annotation := range data["annotations"].([]Annotation) {
					if annotation.ID == annotationID {
						found = append(found, annotation)
					}
				}
				if len(found) == 0 {
					err = fmt.Errorf("没有找到当前协作的原批注")
				}
				data["annotations"] = found
			}
		}
		respond(w, out, err)
		return
	}
	data, _ := json.Marshal(input.Arguments)
	var reply AnnotationReplyInput
	_ = json.Unmarshal(data, &reply)
	s.mu.Lock()
	author := "Codex"
	if s.record.Provider == "claude" {
		author = "Claude Code"
	}
	s.mu.Unlock()
	out, err := s.replyAnnotation(ctx, reply, author)
	respond(w, out, err)
}
