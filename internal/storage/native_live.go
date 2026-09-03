package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"teamcross/internal/domain"
)

const (
	MaxNativeLiveWindows         = 128
	MaxNativeLiveProjectionBytes = 64 << 20
	maxNativeLiveSnapshotBytes   = 20 << 20
	limitedNativeLiveReason      = "实时分享已达到保存上限；此前获准的窗口仍可审阅。"
)

// SaveScopedShareProjection freezes the static projection and, only after an
// explicit prepared binding, atomically admits the exact previewed live window.
// The early write lock serializes this transaction with Follow admission: a
// concurrent poll either follows this seed or makes its preview stale.
func (s *Store) SaveScopedShareProjection(ctx context.Context, shareID string, payload []byte, binding *domain.NativeLiveBinding) error {
	if binding == nil {
		_, err := s.db.ExecContext(ctx, "INSERT INTO share_projections(share_id,payload) VALUES(?,?)", shareID, payload)
		return err
	}
	prepared := *binding
	var err error
	prepared.EntryKinds, err = domain.NormalizeNativeLiveKinds(binding.EntryKinds)
	if err != nil {
		return err
	}
	prepared.Source = domain.NativeLiveSource(prepared.Source)
	if prepared.FollowID == "" || prepared.ExpectedSnapshotID == "" || prepared.FollowEpoch <= 0 {
		return domain.ErrLiveShareFence
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE shares SET expires_at=expires_at WHERE id=?", shareID); err != nil {
		return err
	}
	var threadID string
	var expires int64
	var revoked sql.NullInt64
	if err = tx.QueryRowContext(ctx, "SELECT thread_id,expires_at,revoked_at FROM shares WHERE id=?", shareID).Scan(&threadID, &expires, &revoked); err != nil {
		return mapNotFound(err)
	}
	if revoked.Valid || expires <= millis(time.Now().UTC()) {
		return domain.ErrLiveShareFence
	}
	follow, err := scanFollow(tx.QueryRowContext(ctx, "SELECT "+followColumns+" FROM session_follows WHERE thread_id=? AND id=?", threadID, prepared.FollowID))
	if err != nil {
		if err == domain.ErrNotFound {
			return domain.ErrLiveShareFence
		}
		return err
	}
	if follow.State != "active" || follow.LastPolledAt == nil || follow.Epoch != prepared.FollowEpoch || !domain.SameNativeLiveSource(follow.Source, prepared.Source) {
		return domain.ErrLiveShareFence
	}
	if follow.CurrentSnapshotID != prepared.ExpectedSnapshotID {
		return domain.ErrLiveShareStale
	}
	snapshot, err := s.readNativeLiveSourceSnapshot(ctx, tx, threadID, prepared.ExpectedSnapshotID)
	if err != nil {
		return err
	}
	if !domain.SameNativeLiveSource(snapshot.Source, prepared.Source) {
		return domain.ErrLiveShareFence
	}
	data, err := json.Marshal(prepared)
	if err != nil {
		return err
	}
	var startSeq int64
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(seq),0) FROM events WHERE thread_id=?", threadID).Scan(&startSeq); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO share_projections(share_id,payload) VALUES(?,?)", shareID, payload); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO share_native_live(share_id,follow_id,follow_epoch,binding,start_seq,state,latest_snapshot_id,window_count,projection_bytes) VALUES(?,?,?,?,?,'active',?,0,0)`, shareID, prepared.FollowID, prepared.FollowEpoch, data, startSeq, snapshot.ID); err != nil {
		return err
	}
	if err = s.appendNativeLiveWindow(ctx, tx, shareID, prepared, snapshot); err != nil {
		return err
	}
	var seeded bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM share_live_snapshots WHERE share_id=? AND snapshot_id=?)", shareID, snapshot.ID).Scan(&seeded); err != nil {
		return err
	}
	if !seeded {
		return fmt.Errorf("native live preview exceeds snapshot limits")
	}
	return tx.Commit()
}

// NativeLive state deliberately omits the Follow cursor, raw retry reason and
// local Session metadata. Stopping a Follow never deletes admitted projections.
func (s *Store) GetLiveShareState(ctx context.Context, shareID string) (domain.NativeLiveState, error) {
	var result domain.NativeLiveState
	var data, followData []byte
	var expires int64
	var revoked sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT l.binding,l.state,l.latest_snapshot_id,l.window_count,l.projection_bytes,l.start_seq,s.expires_at,s.revoked_at,f.payload
FROM share_native_live l JOIN shares s ON s.id=l.share_id JOIN session_follows f ON f.id=l.follow_id WHERE l.share_id=?`, shareID).Scan(&data, &result.State, &result.LatestSnapshotID, &result.Count, &result.Bytes, &result.StartSeq, &expires, &revoked, &followData)
	if err != nil {
		return result, mapNotFound(err)
	}
	if err = json.Unmarshal(data, &result.Binding); err != nil {
		return result, err
	}
	result.Binding.Source = domain.NativeLiveSource(result.Binding.Source)
	var follow domain.SessionFollow
	if err = json.Unmarshal(followData, &follow); err != nil {
		return result, err
	}
	switch {
	case revoked.Valid:
		result.State, result.Reason = "revoked", "此分享已撤销。"
	case expires <= millis(time.Now().UTC()):
		result.State, result.Reason = "expired", "此分享已到期。"
	case result.State == "limited":
		result.Reason = limitedNativeLiveReason
	case follow.ID != result.Binding.FollowID || follow.Epoch != result.Binding.FollowEpoch || follow.State == "stopped" || !domain.SameNativeLiveSource(follow.Source, result.Binding.Source):
		result.State, result.Reason = "stopped", "此实时分享已停止更新；此前获准的窗口仍可审阅。"
	case follow.State == "retrying":
		result.State, result.Reason = "retrying", "来源暂时不可读；此前获准的窗口仍可审阅。"
	default:
		result.Reason = "仅发布所选记录类型的只读窗口。"
	}
	return result, nil
}

// HasLiveShareSnapshot is the lightweight notification authorization path. It
// never opens a CAS object; snapshot content must use GetLiveShareSnapshot.
func (s *Store) HasLiveShareSnapshot(ctx context.Context, shareID, snapshotID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM share_live_snapshots m JOIN shares s ON s.id=m.share_id WHERE m.share_id=? AND m.snapshot_id=? AND s.revoked_at IS NULL AND s.expires_at>?)`, shareID, snapshotID, millis(time.Now().UTC())).Scan(&exists)
	return exists, err
}

// GetLiveShareSnapshot authorizes membership before reading CAS. Knowing an
// arbitrary snapshot ID (even from the same native Session) grants no access.
// Old admitted windows remain readable after Follow stop or a budget limit.
func (s *Store) GetLiveShareSnapshot(ctx context.Context, shareID, snapshotID string) (domain.SessionSnapshot, bool, error) {
	var hash, threadID string
	var bindingData []byte
	var size, expires int64
	var revoked sql.NullInt64
	var fully bool
	err := s.db.QueryRowContext(ctx, `SELECT m.projection_object,m.projection_bytes,m.fully_shared,s.thread_id,s.expires_at,s.revoked_at,l.binding
FROM share_live_snapshots m JOIN share_native_live l ON l.share_id=m.share_id JOIN shares s ON s.id=m.share_id
WHERE m.share_id=? AND m.snapshot_id=?`, shareID, snapshotID).Scan(&hash, &size, &fully, &threadID, &expires, &revoked, &bindingData)
	if err != nil {
		return domain.SessionSnapshot{}, false, mapNotFound(err)
	}
	if revoked.Valid || expires <= millis(time.Now().UTC()) {
		return domain.SessionSnapshot{}, false, domain.ErrLiveShareFence
	}
	data, err := s.readNativeLiveObject(hash, size)
	if err != nil {
		return domain.SessionSnapshot{}, false, err
	}
	var binding domain.NativeLiveBinding
	var snapshot domain.SessionSnapshot
	if err = json.Unmarshal(bindingData, &binding); err == nil {
		err = json.Unmarshal(data, &snapshot)
	}
	if err != nil {
		return domain.SessionSnapshot{}, false, err
	}
	if snapshot.ID != snapshotID || snapshot.ThreadID != threadID || !domain.SameNativeLiveSource(snapshot.Source, binding.Source) {
		return domain.SessionSnapshot{}, false, domain.ErrLiveShareFence
	}
	// Revalidate the shape and fixed sanitization contract without reading the
	// private source snapshot, including when serving an old annotation anchor.
	projected, _, err := domain.ProjectNativeSnapshot(snapshot, binding.EntryKinds)
	if err != nil {
		return domain.SessionSnapshot{}, false, err
	}
	canonical, err := json.Marshal(projected)
	if err != nil || !bytes.Equal(data, canonical) {
		return domain.SessionSnapshot{}, false, fmt.Errorf("invalid native live projection")
	}
	return projected, fully, nil
}

func (s *Store) readNativeLiveObject(hash string, size int64) ([]byte, error) {
	if size < 0 || size > maxNativeLiveSnapshotBytes {
		return nil, fmt.Errorf("native live snapshot exceeds byte limit")
	}
	path, err := s.objects.Path(hash)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() != size {
		return nil, fmt.Errorf("native live snapshot object size mismatch")
	}
	data, err := s.objects.Get(hash)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	if int64(len(data)) != size || hex.EncodeToString(digest[:]) != hash {
		return nil, fmt.Errorf("native live snapshot object hash mismatch")
	}
	return data, nil
}

func (s *Store) readNativeLiveSourceSnapshot(ctx context.Context, tx *sql.Tx, threadID, snapshotID string) (domain.SessionSnapshot, error) {
	var hash string
	var size int64
	err := tx.QueryRowContext(ctx, `SELECT p.object_hash,o.size FROM session_snapshots p JOIN objects o ON o.hash=p.object_hash WHERE p.thread_id=? AND p.id=?`, threadID, snapshotID).Scan(&hash, &size)
	if err != nil {
		return domain.SessionSnapshot{}, mapNotFound(err)
	}
	data, err := s.readNativeLiveObject(hash, size)
	if err != nil {
		return domain.SessionSnapshot{}, err
	}
	var snapshot domain.SessionSnapshot
	if err = json.Unmarshal(data, &snapshot); err != nil {
		return snapshot, err
	}
	if snapshot.ID != snapshotID || snapshot.ThreadID != threadID {
		return domain.SessionSnapshot{}, fmt.Errorf("native live snapshot identity mismatch")
	}
	return snapshot, nil
}

func (s *Store) appendNativeLiveWindow(ctx context.Context, tx *sql.Tx, shareID string, binding domain.NativeLiveBinding, snapshot domain.SessionSnapshot) error {
	var count, total int64
	var state string
	if err := tx.QueryRowContext(ctx, "SELECT state,window_count,projection_bytes FROM share_native_live WHERE share_id=?", shareID).Scan(&state, &count, &total); err != nil {
		return err
	}
	if state != "active" {
		return nil
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM share_live_snapshots WHERE share_id=? AND snapshot_id=?)", shareID, snapshot.ID).Scan(&exists); err != nil || exists {
		return err
	}
	projected, fully, err := domain.ProjectNativeSnapshot(snapshot, binding.EntryKinds)
	if err != nil {
		return err
	}
	if !domain.SameNativeLiveSource(snapshot.Source, binding.Source) {
		return domain.ErrLiveShareFence
	}
	data, err := json.Marshal(projected)
	if err != nil {
		return err
	}
	size := int64(len(data))
	if count >= MaxNativeLiveWindows || size > MaxNativeLiveProjectionBytes-total || size > maxNativeLiveSnapshotBytes {
		_, err = tx.ExecContext(ctx, "UPDATE share_native_live SET state='limited' WHERE share_id=?", shareID)
		return err
	}
	hash, err := s.objects.Put(data)
	if err != nil {
		return err
	}
	now := millis(time.Now().UTC())
	if _, err = tx.ExecContext(ctx, "INSERT INTO objects(hash,size,mime,created_at) VALUES(?,?,?,?) ON CONFLICT(hash) DO NOTHING", hash, size, "application/vnd.teamcross.native-live-projection+json", now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO share_live_snapshots(share_id,snapshot_id,projection_object,fully_shared,ordinal,projection_bytes,admitted_at) VALUES(?,?,?,?,?,?,?)`, shareID, snapshot.ID, hash, fully, count+1, size, now); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE share_native_live SET latest_snapshot_id=?,window_count=?,projection_bytes=? WHERE share_id=?", snapshot.ID, count+1, total+size, shareID)
	return err
}

// Called only within CommitFollowPoll after the immutable source is inserted.
// It must never call public Store methods that acquire another DB connection.
func (s *Store) admitNativeLivePoll(ctx context.Context, tx *sql.Tx, follow domain.SessionFollow) error {
	rows, err := tx.QueryContext(ctx, `SELECT l.share_id,l.binding FROM share_native_live l JOIN shares s ON s.id=l.share_id
WHERE l.follow_id=? AND l.follow_epoch=? AND l.state='active' AND s.thread_id=? AND s.revoked_at IS NULL AND s.expires_at>?`, follow.ID, follow.Epoch, follow.ThreadID, millis(time.Now().UTC()))
	if err != nil {
		return err
	}
	type grant struct {
		shareID string
		binding domain.NativeLiveBinding
	}
	grants := []grant{}
	for rows.Next() {
		var item grant
		var data []byte
		if err = rows.Scan(&item.shareID, &data); err == nil {
			err = json.Unmarshal(data, &item.binding)
		}
		if err != nil {
			rows.Close()
			return err
		}
		if item.binding.FollowID == follow.ID && item.binding.FollowEpoch == follow.Epoch && domain.SameNativeLiveSource(item.binding.Source, follow.Source) {
			grants = append(grants, item)
		}
	}
	err = rows.Err()
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	if err != nil || len(grants) == 0 {
		return err
	}
	snapshot, err := s.readNativeLiveSourceSnapshot(ctx, tx, follow.ThreadID, follow.CurrentSnapshotID)
	if err != nil {
		return err
	}
	for _, item := range grants {
		if err = s.appendNativeLiveWindow(ctx, tx, item.shareID, item.binding, snapshot); err != nil {
			return err
		}
	}
	return nil
}
