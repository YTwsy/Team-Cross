package coreclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
)

var errInvitation = errors.New("invitation could not be staged; reopen the invitation after checking Core")
var pendingID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// StageInvitation is a native-only operation. Its credential and invitation
// remain outside the renderer; only the opaque pending ID can enter a page URL.
// A dropped response is never retried because Core may already have accepted it.
func (c *Client) StageInvitation(ctx context.Context, invitation string) (string, error) {
	if len(invitation) == 0 || len(invitation) > 65536 {
		return "", errInvitation
	}
	connection, err := c.discover(ctx)
	if err != nil {
		return "", errConnection
	}
	body, _ := json.Marshal(map[string]string{"invitation": invitation})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, connection.URL+"/api/invitations/pending", bytes.NewReader(body))
	if err != nil {
		return "", errInvitation
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+connection.Token)
	res, err := c.http.Do(req)
	if err != nil {
		return "", errInvitation
	}
	defer res.Body.Close()
	output, err := io.ReadAll(io.LimitReader(res.Body, (16<<10)+1))
	if err != nil || len(output) > 16<<10 || res.StatusCode != http.StatusOK {
		return "", errInvitation
	}
	var result struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(output, &result) != nil || !pendingID.MatchString(result.ID) {
		return "", errInvitation
	}
	return result.ID, nil
}
