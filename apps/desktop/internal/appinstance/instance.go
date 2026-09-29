// Package appinstance owns the desktop shell lock, separately from Core.
// Its message-port name and receipt protocol match the existing macOS shell.
package appinstance

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"syscall"
	"time"

	"teamcross/internal/service"
)

type Request struct {
	Version int      `json:"version"`
	ID      string   `json:"id"`
	URLs    []string `json:"urls"`
}

type receipt struct {
	ID       string `json:"id"`
	Accepted bool   `json:"accepted"`
}

func NewRequest(urls []string) Request {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic("cannot create desktop request identity")
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return Request{1, fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:]), append([]string{}, urls...)}
}

var uuidPattern = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

func valid(request Request) bool {
	if request.Version != 1 || !uuidPattern.MatchString(request.ID) || len(request.URLs) > 32 {
		return false
	}
	for _, raw := range request.URLs {
		if len(raw) > 65536 {
			return false
		}
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "teamcross" || u.Host != "join" || u.User != nil {
			return false
		}
	}
	return true
}

type Instance struct {
	mu       sync.Mutex
	name     string
	file     *os.File
	port     *messagePort
	receive  func(Request) bool
	accepted map[string]time.Time
	closed   bool
	now      func() time.Time
}

func New(directory string) (*Instance, error) {
	directory, err := service.Normalize(directory)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(directory, "app.lock"), os.O_CREATE|os.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("invalid desktop lock")
	}
	// Foundation aliases /private/tmp and /private/var differently from Go.
	// Match the Swift shell's URL normalisation for the port identity while
	// keeping the canonical Go directory for locks and Core discovery.
	identity, err := identityPath(directory)
	if err != nil {
		file.Close()
		return nil, err
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d\n%s", os.Getuid(), identity)))
	return &Instance{name: "io.github.ytwsy.teamcross.app." + hex.EncodeToString(digest[:]), file: file, accepted: make(map[string]time.Time), now: time.Now}, nil
}

// Claim is non-blocking. Never remove app.lock: that would permit two owners
// to lock different inodes during a concurrent launch.
func (i *Instance) Claim(receive func(Request) bool) (bool, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return false, os.ErrClosed
	}
	if i.port != nil {
		return true, nil
	}
	if err := syscall.Flock(int(i.file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return false, nil
		}
		return false, err
	}
	i.receive = receive
	port, err := listen(i.name, i.reply)
	if err != nil {
		syscall.Flock(int(i.file.Fd()), syscall.LOCK_UN)
		return false, err
	}
	i.port = port
	return true, nil
}

func (i *Instance) reply(data []byte) []byte {
	var request Request
	if len(data) > 1<<20 || json.Unmarshal(data, &request) != nil || !valid(request) {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return nil
	}
	now := i.now()
	for id, accepted := range i.accepted {
		if now.Sub(accepted) >= 10*time.Minute {
			delete(i.accepted, id)
		}
	}
	_, duplicate := i.accepted[request.ID]
	ok := duplicate || (len(i.accepted) < 512 && i.receive != nil && i.receive(request))
	if ok {
		i.accepted[request.ID] = now
	}
	result, _ := json.Marshal(receipt{request.ID, ok})
	return result
}

// Forward acknowledges enqueue only. The caller retries this same request ID
// after a lost receipt; it must not generate a new identity for that retry.
func (i *Instance) Forward(request Request) bool {
	if !valid(request) {
		return false
	}
	data, err := json.Marshal(request)
	if err != nil || len(data) > 1<<20 {
		return false
	}
	response := send(i.name, data)
	var out receipt
	return json.Unmarshal(response, &out) == nil && out.ID == request.ID && out.Accepted
}

func (i *Instance) Close() {
	i.mu.Lock()
	if i.closed {
		i.mu.Unlock()
		return
	}
	i.closed = true
	port, file := i.port, i.file
	i.mu.Unlock()
	if port != nil {
		port.close()
	}
	file.Close()
}
