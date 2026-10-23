package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"teamcross/internal/problem"
	"time"
)

type activityPendingRead struct {
	SpaceID  string        `json:"spaceId"`
	Identity string        `json:"identity"`
	Input    ActivityInput `json:"input"`
}
type activityRemoteCache struct {
	Identity string                  `json:"identity"`
	Pages    map[string]ActivityPage `json:"pages"`
}

func (a *App) activityQueuePath() string {
	return filepath.Join(a.Config.DataDir, "member-activity-read-queue.json")
}
func (a *App) loadActivityQueueLocked() (map[string]activityPendingRead, error) {
	q := map[string]activityPendingRead{}
	err := readJSON(a.activityQueuePath(), &q)
	if os.IsNotExist(err) {
		err = nil
	}
	if q == nil {
		return nil, fmt.Errorf("待同步已读记录不完整，原文件已保留")
	}
	return q, err
}
func (a *App) remoteMemberActivity(ctx context.Context, j *Joined, op string, in ActivityInput) (any, error) {
	j.mu.Lock()
	identity := contentHash([]string{j.ID, j.Credential})
	valid := !j.closed && !j.left && !j.ended && j.Credential != ""
	j.mu.Unlock()
	if !valid {
		a.activitySyncMu.Lock()
		_ = os.Remove(filepath.Join(a.Config.DataDir, "collaborations", "member-activity-cache-"+j.ID+".json"))
		a.activitySyncMu.Unlock()
		return nil, fmt.Errorf("成员资格已结束")
	}
	queued := ""
	if op == "read" {
		if len(in.EventIDs) > 500 || len(in.Snapshot) > 4096 {
			return nil, fmt.Errorf("阅读确认范围过大")
		}
		queued = contentHash([]any{identity, in})
		a.activitySyncMu.Lock()
		q, err := a.loadActivityQueueLocked()
		if err == nil {
			if len(q) >= 1000 && q[queued].SpaceID == "" {
				err = fmt.Errorf("待同步已读记录已满")
			} else {
				q[queued] = activityPendingRead{SpaceID: j.ID, Identity: identity, Input: in}
				data, encodeErr := json.Marshal(q)
				if encodeErr != nil {
					err = encodeErr
				} else if len(data) > 8<<20 {
					err = fmt.Errorf("待同步已读记录已达字节上限")
				} else {
					err = writeJSONFile(a.activityQueuePath(), q)
				}
			}
		}
		a.activitySyncMu.Unlock()
		if err != nil {
			return nil, err
		}
	}
	var out json.RawMessage
	err := j.request(ctx, "POST", "/v2/member-activity/"+op, in, &out)
	if queued != "" {
		if err == nil {
			a.removeActivityRead(queued)
			return out, nil
		}
		j.mu.Lock()
		ended := j.ended || j.left
		j.mu.Unlock()
		if ended || problem.Describe(err).Code != "host_unreachable" {
			a.removeActivityRead(queued)
			return nil, err
		}
		return ActivityPage{SpaceID: j.ID, PendingSync: true, Error: "已读，待同步", Groups: []ActivityGroup{}}, nil
	}
	if op != "list" {
		return out, err
	}
	a.activitySyncMu.Lock()
	defer a.activitySyncMu.Unlock()
	cachePath := filepath.Join(a.Config.DataDir, "collaborations", "member-activity-cache-"+j.ID+".json")
	cache := activityRemoteCache{Identity: identity, Pages: map[string]ActivityPage{}}
	_ = readJSON(cachePath, &cache)
	if cache.Identity != identity || cache.Pages == nil {
		cache = activityRemoteCache{Identity: identity, Pages: map[string]ActivityPage{}}
	}
	key := contentHash(in)
	if err == nil {
		var page ActivityPage
		if e := json.Unmarshal(out, &page); e != nil {
			return nil, e
		}
		if len(cache.Pages) >= 12 {
			cache.Pages = map[string]ActivityPage{}
		}
		cached := activityClone(page)
		for i := range cached.Groups {
			g := &cached.Groups[i]
			g.Latest.Preview = ""
			if g.Attention != nil {
				g.Attention.Preview = ""
			}
			for j := range g.Events {
				g.Events[j].Preview = ""
			}
		}
		cache.Pages[key] = cached
		if e := writeJSONFile(cachePath, cache); e != nil {
			page.Error = "动态已同步，但本机离线缓存保存失败"
		}
		return page, nil
	}
	j.mu.Lock()
	ended := j.ended || j.left
	j.mu.Unlock()
	if ended {
		_ = os.Remove(cachePath)
		return nil, err
	}
	if cached, ok := cache.Pages[key]; ok {
		cached.Error = err.Error()
		cached.PendingSync = true
		return cached, nil
	}
	return nil, err
}
func (a *App) removeActivityRead(key string) {
	a.activitySyncMu.Lock()
	defer a.activitySyncMu.Unlock()
	q, err := a.loadActivityQueueLocked()
	if err == nil {
		delete(q, key)
		_ = writeJSONFile(a.activityQueuePath(), q)
	}
}
func (a *App) flushActivityReads(ctx context.Context) {
	a.activitySyncMu.Lock()
	q, err := a.loadActivityQueueLocked()
	a.activitySyncMu.Unlock()
	if err != nil {
		return
	}
	count := 0
	for key, entry := range q {
		if count >= 8 || ctx.Err() != nil {
			return
		}
		count++
		a.mu.Lock()
		j := a.joined[entry.SpaceID]
		a.mu.Unlock()
		if j == nil {
			a.removeActivityRead(key)
			continue
		}
		j.mu.Lock()
		valid := !j.left && !j.ended && contentHash([]string{j.ID, j.Credential}) == entry.Identity
		j.mu.Unlock()
		if !valid {
			a.removeActivityRead(key)
			continue
		}
		call, cancel := context.WithTimeout(ctx, 2*time.Second)
		var out json.RawMessage
		err := j.request(call, "POST", "/v2/member-activity/read", entry.Input, &out)
		cancel()
		if err == nil || problem.Describe(err).Code != "host_unreachable" {
			a.removeActivityRead(key)
		}
	}
}
