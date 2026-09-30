package coreclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"teamcross/internal/uilanguage"
)

// SetLanguage uses the same public preference API as the WebGUI. It performs
// one explicit write, without the native control credential or retries.
func (c *Client) SetLanguage(ctx context.Context, mode string) error {
	if !uilanguage.Valid(mode) {
		return errConnection
	}
	connection, err := c.discover(ctx)
	if err != nil {
		return errConnection
	}
	body, _ := json.Marshal(map[string]string{"mode": mode})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, connection.URL+"/api/ui-language", strings.NewReader(string(body)))
	if err != nil {
		return errConnection
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return errConnection
	}
	defer res.Body.Close()
	var result struct {
		Mode     string `json:"mode"`
		Resolved string `json:"resolved"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&result) != nil || result.Mode != mode || (result.Resolved != uilanguage.Chinese && result.Resolved != uilanguage.English) {
		return errConnection
	}
	return nil
}
