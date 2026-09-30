package coreclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"teamcross/internal/service"
)

var errMissing = errors.New("Core discovery is missing")
var errUnreachable = errors.New("Core is unreachable")
var errStop = errors.New("Core has not confirmed shutdown; check its status before trying again")
var errChanged = errors.New("Core identity changed after quit confirmation")

// Status exposes no control credential. Missing discovery is only considered
// stopped when the Core lock is free; a starting or incompatible Core is an error.
func (c *Client) Status(ctx context.Context) (service.Status, error) {
	s, err := c.probe(ctx)
	s.Token = ""
	if errors.Is(err, errMissing) {
		if idle, check := c.coreIdle(); check == nil && idle {
			return service.Status{}, nil
		}
	}
	if errors.Is(err, errUnreachable) && s.PID > 0 && errors.Is(syscall.Kill(s.PID, 0), syscall.ESRCH) {
		if idle, check := c.coreIdle(); check == nil && idle {
			return service.Status{}, nil
		}
	}
	return s, err
}

func (c *Client) coreIdle() (bool, error) {
	f, err := os.OpenFile(filepath.Join(c.directory, "core.lock"), os.O_CREATE|os.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return false, errConnection
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return false, errConnection
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return false, nil
		}
		return false, errConnection
	}
	return true, nil
}

// Stop operates only on the instance shown to the user. An inactive snapshot
// uses force=false so Core rejects newly started collaborations. No stop POST
// is retried, including after an accepted write loses its response.
func (c *Client) Stop(ctx context.Context, expected service.Status, force bool) error {
	s, err := c.probe(ctx)
	if err != nil {
		return err
	}
	if !expected.Running || s.Instance != expected.Instance || s.PID != expected.PID || s.URL != expected.URL || s.DataDir != expected.DataDir {
		return errChanged
	}
	body := `{"force":false}`
	if force {
		body = `{"force":true}`
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL+"/api/control/stop", strings.NewReader(body))
	if err != nil {
		return errStop
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return errStop
	}
	defer res.Body.Close()
	var receipt struct {
		Stopping bool `json:"stopping"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&receipt) != nil || !receipt.Stopping {
		return errStop
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		// A newer Core must not be mistaken for the instance being stopped.
		if current, err := service.Read(c.directory); err == nil && current.Instance != expected.Instance {
			return errChanged
		}
		idle, err := c.coreIdle()
		if err != nil {
			return err
		}
		if idle {
			return nil
		}
		select {
		case <-ctx.Done():
			return errStop
		case <-ticker.C:
		}
	}
}
