package ext

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// SDKImportName is the bare specifier extensions import. It resolves via a
// generated import map to the local SDK module, so --deny-import (no remote
// modules) holds without breaking the SDK (R6).
const SDKImportName = "@aperture/sdk"

// WriteImportMap writes extDir/.import-map.json mapping the SDK specifier to the
// local module and returns its path.
func WriteImportMap(extDir, sdkModulePath string) (string, error) {
	m := map[string]any{"imports": map[string]string{SDKImportName: sdkModulePath}}
	b, _ := json.MarshalIndent(m, "", "  ")
	p := filepath.Join(extDir, ".import-map.json")
	return p, os.WriteFile(p, b, 0o600)
}

// BuildSpec assembles the supervisor Spec for a backend extension from its resolved
// manifest authority (d), runtime paths, and identity. This is the ONE place a
// manifest's permissions become Deno flags: deny-by-default, allowing only the
// extension's own data dir, the read-only SDK + import map, and its declared
// network hosts (plus the Aperture API host so the SDK can reach it). Everything
// else — subprocess, FFI, env, remote import, the rest of the filesystem — is
// denied, and the process runs as a non-root per-extension uid with the memory /
// fd / proc / oom bounds from the supervisor defaults.
func BuildSpec(id, entryAbs, extDir, sdkModulePath, importMapPath, apiBase, token string, uid int, d Derived) Spec {
	readPaths := strings.Join([]string{extDir, filepath.Dir(sdkModulePath), importMapPath}, ",")
	flags := []string{
		"--no-prompt",
		"--deny-run", "--deny-ffi", "--deny-sys", "--deny-import",
		// Env is allow-listed to exactly the two vars the SDK reads for its token
		// and base URL — NOT --deny-env, or the SDK's Deno.env.get() throws
		// NotCapable and no backend extension can authenticate. The child's env is
		// replaced wholesale by Spec.Env below (supervisor: cmd.Env = spec.Env), so
		// this exposes only the extension's own token and the public API base — no
		// host environment leaks through.
		"--allow-env=APERTURE_TOKEN,APERTURE_API,APERTURE_EXT_ID",
		"--allow-read=" + readPaths,
		"--allow-write=" + extDir,
		"--import-map=" + importMapPath,
	}
	nets := append([]string{}, d.NetHosts...)
	if h := hostOf(apiBase); h != "" {
		nets = append(nets, h) // the SDK must reach the Aperture API
	}
	if len(nets) > 0 {
		flags = append(flags, "--allow-net="+strings.Join(nets, ","))
	} else {
		flags = append(flags, "--deny-net")
	}
	return Spec{
		ID:        id,
		Entry:     entryAbs,
		DenoFlags: flags,
		WorkDir:   extDir,
		Env: []string{
			"APERTURE_TOKEN=" + token,
			"APERTURE_API=" + apiBase,
			"APERTURE_EXT_ID=" + id, // so the SDK can address its own private KV namespace
			"HOME=" + extDir,
			"DENO_DIR=" + filepath.Join(extDir, ".deno"),
			"NO_COLOR=1",
		},
		UID: uid, GID: uid,
		MemMB: DefaultMemMB, NOFILE: DefaultNOFILE, NPROC: DefaultNPROC, OOMScoreAdj: DefaultOOMScoreAdj,
	}
}

func hostOf(base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	return u.Host
}
