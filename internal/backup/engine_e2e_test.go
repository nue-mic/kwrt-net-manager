package backup

import (
	"context"
	"io"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/webdav"
)

// newWebDAVServer spins up an in-memory WebDAV server for end-to-end tests and
// returns its base URL.
func newWebDAVServer(t *testing.T) string {
	t.Helper()
	h := &webdav.Handler{
		FileSystem: webdav.NewMemFS(),
		LockSystem: webdav.NewMemLS(),
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

func webdavChannel(baseURL string) Channel {
	return Channel{
		ID:     "ch_test",
		Name:   "test-dav",
		Kind:   KindWebDAV,
		WebDAV: &WebDAVConfig{BaseURL: baseURL, Prefix: "frpc"},
	}
}

func TestWebDAVUploaderRoundTrip(t *testing.T) {
	up, err := newWebDAVUploader(webdavChannel(newWebDAVServer(t)).WebDAV)
	if err != nil {
		t.Fatalf("newWebDAVUploader: %v", err)
	}
	ctx := context.Background()

	if err := up.Test(ctx); err != nil {
		t.Fatalf("Test: %v", err)
	}
	// List on an empty/missing prefix must be empty, not an error.
	if objs, err := up.List(ctx, "backups/d/"); err != nil || len(objs) != 0 {
		t.Fatalf("empty List: objs=%v err=%v", objs, err)
	}

	keys := []string{
		"backups/d/2026/06/frpcmgr-20260613-030001.zip",
		"backups/d/2026/06/frpcmgr-20260613-030002.zip",
		"backups/d/2026/07/frpcmgr-20260701-030000.zip",
	}
	for _, k := range keys {
		if err := up.Put(ctx, k, []byte("zip-"+k)); err != nil {
			t.Fatalf("Put %s: %v", k, err)
		}
	}

	objs, err := up.List(ctx, "backups/d/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(objs) != 3 {
		t.Fatalf("List returned %d objects, want 3: %+v", len(objs), objs)
	}
	// Keys are channel-relative (prefix stripped).
	for _, o := range objs {
		if got := o.Key; got != "" && got[:len("backups/")] != "backups/" {
			t.Fatalf("unexpected non-relative key: %q", o.Key)
		}
	}

	if err := up.Delete(ctx, keys[0]); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	objs, _ = up.List(ctx, "backups/d/")
	if len(objs) != 2 {
		t.Fatalf("after delete: %d objects, want 2", len(objs))
	}
}

// ---- engine fakes ----

type fakeSource struct{ data []byte }

func (f fakeSource) BuildBackupZip(w io.Writer) error {
	_, err := w.Write(f.data)
	return err
}

type fakeStore struct {
	ch Channel
	sc Schedule
}

func (s fakeStore) GetBackupChannel(id string) (Channel, bool) {
	if id == s.ch.ID {
		return s.ch, true
	}
	return Channel{}, false
}
func (s fakeStore) ListBackupSchedules() []Schedule { return []Schedule{s.sc} }
func (s fakeStore) GetBackupSchedule(id string) (Schedule, bool) {
	if id == s.sc.ID {
		return s.sc, true
	}
	return Schedule{}, false
}

type fakeRecorder struct {
	mu   sync.Mutex
	runs []RunRecord
}

func (r *fakeRecorder) AppendBackupRun(rec RunRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs = append(r.runs, rec)
	return nil
}
func (r *fakeRecorder) snapshot() []RunRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]RunRecord(nil), r.runs...)
}

// TestSchedulerRunNowEndToEnd drives the full engine: RunNow → BuildBackupZip →
// upload to WebDAV → record a success run, and verifies the object landed.
func TestSchedulerRunNowEndToEnd(t *testing.T) {
	base := newWebDAVServer(t)
	ch := webdavChannel(base)
	sc := Schedule{
		ID: "sc_test", Name: "每日", Enabled: true,
		Cron: "@daily", ChannelID: ch.ID, Retention: 0,
	}
	rec := &fakeRecorder{}
	s := NewScheduler(fakeStore{ch: ch, sc: sc}, fakeSource{data: []byte("PK-fake-zip")}, rec, nil, nil, "testhost")
	defer s.Stop()

	if err := s.RunNow(sc.ID); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	// Wait for the async job to finish.
	var got RunRecord
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runs := rec.snapshot(); len(runs) > 0 {
			got = runs[0]
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got.ID == "" {
		t.Fatal("no run recorded within timeout")
	}
	if got.Status != StatusSuccess {
		t.Fatalf("run status = %q, err=%q", got.Status, got.Error)
	}
	if got.Trigger != TriggerManual {
		t.Fatalf("trigger = %q, want manual", got.Trigger)
	}
	if got.SizeBytes != int64(len("PK-fake-zip")) {
		t.Fatalf("size = %d", got.SizeBytes)
	}
	// The uploaded object must exist under the schedule's retention prefix.
	up, _ := newWebDAVUploader(ch.WebDAV)
	objs, err := up.List(context.Background(), retentionPrefix(sc.EffectiveTemplate(), sc.Name, "testhost"))
	if err != nil || len(objs) != 1 {
		t.Fatalf("expected 1 uploaded object, got %d err=%v", len(objs), err)
	}
}

// TestSchedulerRetention seeds more objects than the retention limit and checks
// only the newest N survive.
func TestSchedulerRetention(t *testing.T) {
	base := newWebDAVServer(t)
	ch := webdavChannel(base)
	sc := Schedule{ID: "sc_r", Name: "保留", Enabled: true, Cron: "@daily", ChannelID: ch.ID, Retention: 2}
	s := NewScheduler(fakeStore{ch: ch, sc: sc}, fakeSource{}, &fakeRecorder{}, nil, nil, "h")
	defer s.Stop()

	up, _ := newWebDAVUploader(ch.WebDAV)
	ctx := context.Background()
	// 4 timestamped backups under the schedule's stable prefix.
	prefix := retentionPrefix(sc.EffectiveTemplate(), sc.Name, "h")
	seed := []string{
		prefix + "2026/06/frpcmgr-20260613-030001.zip",
		prefix + "2026/06/frpcmgr-20260613-030002.zip",
		prefix + "2026/06/frpcmgr-20260613-030003.zip",
		prefix + "2026/06/frpcmgr-20260613-030004.zip",
	}
	for _, k := range seed {
		if err := up.Put(ctx, k, []byte("x")); err != nil {
			t.Fatalf("seed Put: %v", err)
		}
	}

	s.applyRetention(ctx, up, sc)

	objs, err := up.List(ctx, prefix)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(objs) != 2 {
		t.Fatalf("retention kept %d, want 2: %+v", len(objs), objs)
	}
	// The two newest (003, 004) must remain.
	remaining := map[string]bool{}
	for _, o := range objs {
		remaining[o.Key] = true
	}
	if !remaining[seed[2]] || !remaining[seed[3]] {
		t.Fatalf("retention deleted the wrong objects: %+v", remaining)
	}
}

// TestRetentionScopedToSchedule verifies retention only ever deletes objects
// this schedule itself produced — foreign files and other-shaped names in the
// same directory must survive even when retention is exceeded.
func TestRetentionScopedToSchedule(t *testing.T) {
	base := newWebDAVServer(t)
	ch := webdavChannel(base)
	sc := Schedule{ID: "sc_s", Name: "每日", Enabled: true, Cron: "@daily", ChannelID: ch.ID, Retention: 1}
	s := NewScheduler(fakeStore{ch: ch, sc: sc}, fakeSource{}, &fakeRecorder{}, nil, nil, "h")
	defer s.Stop()

	up, _ := newWebDAVUploader(ch.WebDAV)
	ctx := context.Background()
	prefix := retentionPrefix(sc.EffectiveTemplate(), sc.Name, "h")
	own := []string{
		prefix + "2026/06/frpcmgr-20260613-030001.zip",
		prefix + "2026/06/frpcmgr-20260613-030002.zip",
	}
	foreign := []string{
		prefix + "2026/06/manual-keep.zip", // wrong filename shape
		prefix + "important.zip",           // no date dirs
	}
	for _, k := range append(append([]string{}, own...), foreign...) {
		if err := up.Put(ctx, k, []byte("x")); err != nil {
			t.Fatalf("seed Put %s: %v", k, err)
		}
	}

	s.applyRetention(ctx, up, sc) // Retention=1 → delete 1 oldest OWN, touch nothing else

	objs, _ := up.List(ctx, prefix)
	got := map[string]bool{}
	for _, o := range objs {
		got[o.Key] = true
	}
	for _, k := range foreign {
		if !got[k] {
			t.Fatalf("retention deleted a foreign file: %s", k)
		}
	}
	ownLeft := 0
	for _, k := range own {
		if got[k] {
			ownLeft++
		}
	}
	if ownLeft != 1 {
		t.Fatalf("own objects remaining = %d, want 1", ownLeft)
	}
}
