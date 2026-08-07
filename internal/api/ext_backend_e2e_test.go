package api

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TheOriUHD/aperture/internal/ext"
	"github.com/TheOriUHD/aperture/internal/store"
)

// TestBackendExtensionRunsEndToEnd spawns a REAL Deno backend extension through the
// real spawn path — generated import map, setuid-drop to a non-root uid, the rlimit
// trampoline (via this test binary's TestMain), and Deno itself — and asserts it
// reaches AND STAYS in a running state.
//
// This is the test that was missing. The supervisor's other tests launch a stand-in
// command via the argv seam, so they never exercised a non-root child reading the
// host-written import map — and the ownership bug (root-owned .import-map.json,
// unreadable by the dropped uid → crash-loop) shipped. It needs Deno + root (to
// setuid-drop); it self-skips otherwise, so a normal `go test` is unaffected.
func TestBackendExtensionRunsEndToEnd(t *testing.T) {
	denoPath, err := exec.LookPath("deno")
	if err != nil {
		t.Skip("deno not on PATH")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root to setuid-drop to a non-root extension uid")
	}
	const testUID = 65600

	s := newTestServerNoRouter(t)
	mustDo(t, s.st.CreateUser(&store.User{ID: "u1", Email: "u@x", Name: "U", Role: store.RoleAdmin}))

	// The SDK module the import map resolves to must be OS-readable by the dropped
	// uid, so keep it under DataDir/sdk (0755 dir, 0444 file).
	sdkDir := filepath.Join(s.cfg.DataDir, "sdk")
	mustDo(t, os.MkdirAll(sdkDir, 0o755))
	sdk, err := os.ReadFile("../../deploy/aperture-sdk/mod.ts")
	mustDo(t, err)
	sdkPath := filepath.Join(sdkDir, "mod.ts")
	mustDo(t, os.WriteFile(sdkPath, sdk, 0o444))

	sup := ext.NewSupervisor(denoPath, s.log)
	t.Cleanup(sup.StopAll)
	s.SetExtensions(sup, denoPath, sdkPath, "http://127.0.0.1:8080")

	// The fixture is the SHIPPED canonical backend example, so this test is also the
	// standing proof that examples/motion-webhook actually runs end to end — the path
	// that silently didn't work until the import-map ownership fix. It imports the SDK
	// (exercising the import map under --deny-import), subscribes to the change feed
	// (which keeps the process alive, retrying with backoff even without a server here),
	// and prints its ready marker.
	const id = "com.example.motion-webhook"
	exampleDir := filepath.Join("..", "..", "examples", "motion-webhook")
	manifestBytes, err := os.ReadFile(filepath.Join(exampleDir, "manifest.json"))
	mustDo(t, err)
	mainTS, err := os.ReadFile(filepath.Join(exampleDir, "backend", "main.ts"))
	mustDo(t, err)
	manifest := string(manifestBytes)
	m, err := ext.Parse(manifestBytes)
	mustDo(t, err)
	d, err := m.Validate()
	mustDo(t, err)

	extDir := s.extDir(id)
	mustDo(t, os.MkdirAll(filepath.Join(extDir, "backend"), 0o700))
	mustDo(t, os.WriteFile(filepath.Join(extDir, "backend", "main.ts"), mainTS, 0o600))
	mustDo(t, os.WriteFile(filepath.Join(extDir, "manifest.json"), manifestBytes, 0o600))

	// Ownership + traversability exactly like production: the extension dir tree is
	// owned by its uid; every ancestor up to the temp root is traversable (0711) so
	// the dropped child can reach it (prod does this via the entrypoint on /data).
	chownR(extDir, testUID)
	mustDo(t, os.Chmod(extDir, 0o700))
	for d := filepath.Dir(extDir); d != os.TempDir() && d != "/" && filepath.Dir(d) != d; d = filepath.Dir(d) {
		_ = os.Chmod(d, 0o711)
	}
	_ = os.Chmod(sdkDir, 0o755)

	ex := &store.Extension{
		ID: id, Name: m.Name, Version: m.Version, Source: "sideload",
		ManifestJSON: manifest, Scopes: d.Scopes, CameraGrants: d.CameraGrants,
		Enabled: true, ExtUID: testUID, InstalledBy: "u1",
	}
	mustDo(t, s.st.CreateExtension(ex))

	s.spawnIfBackend(ex) // the path under test: writes + chowns the import map, spawns

	// 1) reach running.
	deadline := time.Now().Add(25 * time.Second)
	for {
		st, reason := sup.Status(id)
		if st == ext.StateRunning {
			break
		}
		if st == ext.StateFailed {
			t.Fatalf("backend failed to start: %s\n--- logs ---\n%s", reason, strings.Join(sup.Logs(id), "\n"))
		}
		if time.Now().After(deadline) {
			t.Fatalf("backend never reached running (state=%s)\n--- logs ---\n%s", st, strings.Join(sup.Logs(id), "\n"))
		}
		time.Sleep(150 * time.Millisecond)
	}

	// 2) stay running. A crash-looper flashes "running" before each crash (state is
	// set before cmd.Run), so settle and require it hasn't degraded or logged a crash
	// — that is what distinguishes a healthy backend from the import-map bug.
	time.Sleep(4 * time.Second)
	if st, reason := sup.Status(id); st != ext.StateRunning {
		t.Fatalf("backend did not stay running: state=%s %s\n--- logs ---\n%s", st, reason, strings.Join(sup.Logs(id), "\n"))
	}
	logs := strings.Join(sup.Logs(id), "\n")
	if strings.Contains(logs, "exited after") || strings.Contains(logs, "giving up") {
		t.Fatalf("backend crashed at least once (the ownership bug would do exactly this):\n%s", logs)
	}
	if !strings.Contains(logs, "motion-webhook ready") {
		t.Errorf("expected the example's ready marker in the logs:\n%s", logs)
	}
}
