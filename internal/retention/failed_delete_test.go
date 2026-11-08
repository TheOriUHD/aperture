package retention

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/config"
	"github.com/TheOriUHD/aperture/internal/storage"
	"github.com/TheOriUHD/aperture/internal/store"
)

// share is a janitor with an "SMB" location whose backend is really a local folder:
// footage takes the same relative-path route through the registry as on a NAS, and a
// delete can be made to fail for real (see jam), even when the tests run as root.
type share struct {
	j    *Janitor
	st   *store.Store
	loc  *store.StorageLocation
	root string
	logs *bytes.Buffer
}

func newShare(t *testing.T) *share {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "j.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	root := t.TempDir()
	loc := &store.StorageLocation{Name: "NAS", Type: store.LocationSMB,
		Config: map[string]any{}, Enabled: true}
	if err := st.CreateStorageLocation(loc); err != nil {
		t.Fatal(err)
	}
	reg := storage.NewRegistry(func(id string) (*storage.Spec, error) {
		if id != loc.ID {
			return nil, errors.New("no such location")
		}
		return &storage.Spec{Type: storage.TypeLocal, Config: map[string]any{"path": root}}, nil
	})
	logs := &bytes.Buffer{}
	j := New(&config.Config{}, st, reg, slog.New(slog.NewTextHandler(logs, nil)))
	j.du = func(string) (int64, int64, bool) { return 1 << 40, 1 << 39, true }
	j.shareDu = func(string) (int64, int64, bool) { return 1 << 40, 1 << 39, true }
	if err := st.CreateCamera(&store.Camera{ID: "c1", Name: "c1", RetentionDays: 7, LocationID: loc.ID}); err != nil {
		t.Fatal(err)
	}
	return &share{j: j, st: st, loc: loc, root: root, logs: logs}
}

// seg records one 2-byte segment on the share, its file included.
func (sh *share) seg(t *testing.T, start int64) *store.Segment {
	t.Helper()
	s := &store.Segment{ID: store.NewID(), CameraID: "c1", LocationID: sh.loc.ID,
		Path: fmt.Sprintf("c1/%d.ts", start), StartTS: start, EndTS: start + 10_000,
		DurationMS: 10_000, SizeBytes: 2}
	sh.unjam(t, s.Path)
	if err := sh.st.InsertSegment(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func (sh *share) full(rel string) string { return filepath.Join(sh.root, rel) }

// jam makes a file undeletable: a directory with something in it can't be removed.
func (sh *share) jam(t *testing.T, rel string) {
	t.Helper()
	if err := os.Remove(sh.full(rel)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sh.full(rel), "x"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func (sh *share) unjam(t *testing.T, rel string) {
	t.Helper()
	if err := os.RemoveAll(sh.full(rel)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(sh.full(rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sh.full(rel), []byte("xx"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (sh *share) rows(t *testing.T) int {
	t.Helper()
	stat, err := sh.st.CameraStat("c1")
	if err != nil {
		t.Fatal(err)
	}
	return int(stat.Count)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// The bug behind hundreds of GB of orphans: the row went whether or not the file did.
func TestFailedDeleteKeepsTheRow(t *testing.T) {
	sh := newShare(t)
	s := sh.seg(t, 1000)
	sh.jam(t, s.Path)

	if sh.j.delete(s) {
		t.Error("delete reported success for a file still on the share")
	}
	if sh.rows(t) != 1 {
		t.Error("the row went while the file stayed: that file is now invisible to retention")
	}

	sh.unjam(t, s.Path)
	if !sh.j.delete(s) {
		t.Error("the retry failed on a file that can now be deleted")
	}
	if sh.rows(t) != 0 || exists(sh.full(s.Path)) {
		t.Errorf("after the retry: rows=%d, file left=%v", sh.rows(t), exists(sh.full(s.Path)))
	}
}

// Nothing has to remember a failed delete: the expired row is the retry.
func TestNextPassRetriesAFailedDelete(t *testing.T) {
	sh := newShare(t)
	old := time.Now().Add(-30 * 24 * time.Hour).UnixMilli()
	s := sh.seg(t, old)
	sh.jam(t, s.Path)

	sh.j.enforce()
	if sh.rows(t) != 1 {
		t.Fatal("the pass dropped a row whose file it couldn't delete")
	}

	sh.unjam(t, s.Path)
	sh.j.enforce()
	if sh.rows(t) != 0 || exists(sh.full(s.Path)) {
		t.Errorf("next pass: rows=%d, file left=%v", sh.rows(t), exists(sh.full(s.Path)))
	}
}

// A wiped folder must not leave rows retrying forever.
func TestAlreadyGoneCountsAsDeleted(t *testing.T) {
	sh := newShare(t)
	s := sh.seg(t, 1000)
	if err := os.Remove(sh.full(s.Path)); err != nil {
		t.Fatal(err)
	}

	if !sh.j.delete(s) {
		t.Error("a file that is already gone should count as deleted")
	}
	if sh.rows(t) != 0 {
		t.Error("the row of a missing file should go")
	}
}

func TestUnreachableBackendKeepsTheRow(t *testing.T) {
	sh := newShare(t)
	s := sh.seg(t, 1000)
	s.LocationID = "no-such-location"

	if sh.j.delete(s) {
		t.Error("delete reported success with no backend to delete through")
	}
	if sh.rows(t) != 1 {
		t.Error("the row went although nothing could have deleted the file")
	}
}

// A segment's death includes its mirrors, so a mirror that won't go keeps it all.
func TestFailedMirrorDeleteKeepsTheSegment(t *testing.T) {
	sh := newShare(t)
	dir := t.TempDir()
	local := fileSeg(t, sh.st, dir, "c1", store.DefaultLocationID, 1000)
	rel := "c1/mirror.ts"
	sh.unjam(t, rel)
	sh.jam(t, rel)
	if err := sh.st.InsertSegmentCopy(&store.SegmentCopy{SegmentID: local.ID, LocationID: sh.loc.ID, Path: rel, SizeBytes: 2}); err != nil {
		t.Fatal(err)
	}

	if sh.j.delete(local) {
		t.Error("delete reported success with a mirror still on the share")
	}
	copies, err := sh.st.CopiesForSegment(local.ID)
	if err != nil || len(copies) != 1 {
		t.Errorf("copy rows = %d (%v), want the one that couldn't be deleted", len(copies), err)
	}
	if sh.rows(t) != 1 || !exists(local.Path) {
		t.Errorf("segment row kept=%v, canonical file kept=%v", sh.rows(t) == 1, exists(local.Path))
	}
}

// Promotion used to move the row first, so a failed delete orphaned the old file.
func TestFailedPromotionMovesNothing(t *testing.T) {
	sh := newShare(t)
	s := sh.seg(t, 1000)
	sh.jam(t, s.Path)
	mirror := filepath.Join(t.TempDir(), "mirror.ts")
	if err := os.WriteFile(mirror, []byte("xx"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := sh.st.InsertSegmentCopy(&store.SegmentCopy{SegmentID: s.ID, LocationID: store.DefaultLocationID, Path: mirror, SizeBytes: 2}); err != nil {
		t.Fatal(err)
	}

	if sh.j.evictCanonical(s) {
		t.Error("eviction reported success with the old file still on the share")
	}
	got, err := sh.st.SegmentByID(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LocationID != sh.loc.ID || got.Path != s.Path {
		t.Errorf("segment moved to %q %q: nothing records the old file now", got.LocationID, got.Path)
	}

	sh.unjam(t, s.Path)
	if !sh.j.evictCanonical(s) {
		t.Fatal("the retry failed")
	}
	if got, err = sh.st.SegmentByID(s.ID); err != nil {
		t.Fatal(err)
	}
	if got.Path != mirror || exists(sh.full(s.Path)) {
		t.Errorf("after the retry: path %q, old file left=%v", got.Path, exists(sh.full(s.Path)))
	}
}

func TestFailedCopyEvictionKeepsTheCopy(t *testing.T) {
	sh := newShare(t)
	local := fileSeg(t, sh.st, t.TempDir(), "c1", store.DefaultLocationID, 1000)
	rel := "c1/mirror.ts"
	sh.unjam(t, rel)
	sh.jam(t, rel)
	if err := sh.st.InsertSegmentCopy(&store.SegmentCopy{SegmentID: local.ID, LocationID: sh.loc.ID, Path: rel, SizeBytes: 2}); err != nil {
		t.Fatal(err)
	}

	sh.j.enforceDestinationRetention("c1", sh.loc.ID, time.Now().UnixMilli())

	if copies, _ := sh.st.CopiesForSegment(local.ID); len(copies) != 1 {
		t.Error("the copy row went while its file stayed")
	}
}

// More undeletable footage than a batch sits at the front of every oldest-first
// loop. The loops have to read past it or one bad stretch stops retention for good.
func TestCapAndQuotaReadPastUndeletableFootage(t *testing.T) {
	const jammed, deletable = deleteBatch + 10, 5
	setup := func(t *testing.T) (*share, []*store.Segment) {
		sh := newShare(t)
		var segs []*store.Segment
		for i := 0; i < jammed+deletable; i++ {
			s := sh.seg(t, int64(1000+i))
			if i < jammed {
				sh.jam(t, s.Path)
			}
			segs = append(segs, s)
		}
		return sh, segs
	}
	check := func(t *testing.T, sh *share, segs []*store.Segment) {
		t.Helper()
		if got, want := sh.rows(t), jammed+deletable-2; got != want {
			t.Fatalf("rows = %d, want %d: two deletable segments should have gone", got, want)
		}
		for _, s := range segs[:jammed] {
			if _, err := sh.st.SegmentByID(s.ID); err != nil {
				t.Fatalf("undeletable segment %s lost its row", s.Path)
			}
		}
	}
	total := int64(2 * (jammed + deletable))

	t.Run("location cap", func(t *testing.T) {
		sh, segs := setup(t)
		sh.j.enforceLocationCap(sh.loc.ID, total-4, false)
		check(t, sh, segs)
	})
	t.Run("camera quota", func(t *testing.T) {
		sh, segs := setup(t)
		sh.j.enforceQuota("c1", total-4)
		check(t, sh, segs)
	})
}

// Once a location has used up its budget for a pass it isn't tried again, so a share
// that is down can't keep every other camera waiting behind it.
func TestALocationOutOfBudgetIsLeftAloneUntilNextPass(t *testing.T) {
	sh := newShare(t)
	s := sh.seg(t, 1000)
	sh.j.deleteTrouble = map[string]*deleteTrouble{
		sh.loc.ID: {failed: 1, spent: deleteFailureBudget, first: errors.New("share is down")},
	}

	if sh.j.delete(s) || !exists(sh.full(s.Path)) {
		t.Fatal("deleted through a location that was out of budget")
	}
	if got := sh.j.deleteTrouble[sh.loc.ID].skipped; got != 1 {
		t.Errorf("notTried = %d, want 1", got)
	}
	other := fileSeg(t, sh.st, t.TempDir(), "c1", store.DefaultLocationID, 2000)
	if !sh.j.delete(other) {
		t.Error("another location was held up by this one")
	}

	sh.j.enforce() // a new pass starts with a new budget
	if !sh.j.delete(s) {
		t.Error("the budget carried over into the next pass")
	}
}

func TestFailuresSpendTheBudget(t *testing.T) {
	sh := newShare(t)
	s := sh.seg(t, 1000)
	sh.jam(t, s.Path)

	sh.j.delete(s)
	sh.j.delete(s)

	got := sh.j.deleteTrouble[sh.loc.ID]
	if got == nil || got.failed != 2 || got.spent <= 0 {
		t.Fatalf("trouble = %+v, want two failures and some time spent", got)
	}
}

func TestAPassLogsOneLinePerLocation(t *testing.T) {
	sh := newShare(t)
	old := time.Now().Add(-30 * 24 * time.Hour).UnixMilli()
	for i := 0; i < 20; i++ {
		s := sh.seg(t, old+int64(i))
		sh.jam(t, s.Path)
	}

	sh.j.enforce()

	logged := sh.logs.String()
	if n := strings.Count(logged, "could not delete footage"); n != 1 {
		t.Errorf("%d log lines for one pass, want 1:\n%s", n, logged)
	}
	if !strings.Contains(logged, "failed=20") {
		t.Errorf("the line should say how many failed:\n%s", logged)
	}

	sh.logs.Reset()
	clean := newShare(t)
	clean.j.enforce()
	if strings.Contains(clean.logs.String(), "could not delete") {
		t.Error("a clean pass reported failed deletes")
	}
}

func TestShareFreeFloor(t *testing.T) {
	const gib = int64(1) << 30
	if got := shareFreeFloor(20 << 40); got != 5*gib {
		t.Errorf("20 TiB share: floor %d, want 5 GiB and no percentage of someone else's pool", got)
	}
	if got := shareFreeFloor(8 * gib); got != 2*gib {
		t.Errorf("8 GiB share: floor %d, want a quarter of it", got)
	}
}

func TestShareFreeSpaceNet(t *testing.T) {
	const gib = int64(1) << 30
	// Recent footage, so it is the net removing it and not ordinary age retention.
	base := time.Now().Add(-time.Hour).UnixMilli()
	// A 1 TiB share where each segment deleted frees 200 MiB.
	shrinking := func(sh *share, t *testing.T, startFree int64, start int) func(string) (int64, int64, bool) {
		return func(string) (int64, int64, bool) {
			return 1 << 40, startFree + int64(start-sh.rows(t))*(200<<20), true
		}
	}

	t.Run("low: evicts oldest and says so", func(t *testing.T) {
		sh := newShare(t)
		const n = deleteBatch + 10
		for i := 0; i < n; i++ {
			sh.seg(t, base+int64(i))
		}
		sh.j.shareDu = shrinking(sh, t, gib, n)

		sh.j.enforce()

		if got := sh.rows(t); got != 10 {
			t.Errorf("rows = %d, want one batch evicted, which clears the floor", got)
		}
		if !strings.Contains(sh.logs.String(), "network share is nearly full") {
			t.Errorf("a nearly full share went unreported:\n%s", sh.logs.String())
		}
	})
	t.Run("low with undeletable footage reads past it", func(t *testing.T) {
		sh := newShare(t)
		const jammed, n = deleteBatch + 10, deleteBatch + 70
		for i := 0; i < n; i++ {
			s := sh.seg(t, base+int64(i))
			if i < jammed {
				sh.jam(t, s.Path)
			}
		}
		sh.j.shareDu = shrinking(sh, t, gib, n)

		sh.j.enforce()

		if got := sh.rows(t); got == n {
			t.Error("the net gave up behind undeletable footage")
		}
		if got := sh.rows(t); got < jammed {
			t.Errorf("rows = %d: undeletable footage lost its rows", got)
		}
	})
	t.Run("full with nothing to evict is still reported", func(t *testing.T) {
		sh := newShare(t)
		sh.j.shareDu = func(string) (int64, int64, bool) { return 1 << 40, gib, true }

		sh.j.enforce()

		if !strings.Contains(sh.logs.String(), "removed=0") {
			t.Errorf("should report a full share it can't help:\n%s", sh.logs.String())
		}
	})
	for _, c := range []struct {
		name        string
		total, free int64
		ok          bool
	}{
		{"above the floor", 1 << 40, 6 * gib, true},
		{"above the floor on a huge pool, where 7% would have fired", 20 << 40, 6 * gib, true},
		{"share can't report capacity", 0, 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			sh := newShare(t)
			sh.seg(t, base)
			sh.j.shareDu = func(string) (int64, int64, bool) { return c.total, c.free, c.ok }

			sh.j.enforce()

			if sh.rows(t) != 1 {
				t.Error("footage was evicted")
			}
			if strings.Contains(sh.logs.String(), "nearly full") {
				t.Error("reported a problem that isn't there")
			}
		})
	}
}
