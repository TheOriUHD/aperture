package api

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/ext"
	"github.com/TheOriUHD/aperture/internal/store"
)

// extUIDBase is the first per-extension non-root uid; each install gets the next.
const extUIDBase = 65600

// SetExtensions wires the backend supervisor + the runtime paths a Deno launch
// spec needs (called from main). Nil supervisor = extensions disabled (tests).
func (s *Server) SetExtensions(sup *ext.Supervisor, denoPath, sdkModule, apiBase string) {
	s.extSup, s.denoPath, s.sdkModule, s.apiBase = sup, denoPath, sdkModule, apiBase
}

func (s *Server) extEnabled() bool { v, _ := s.st.GetSetting("ext.enabled"); return v == "1" }
func (s *Server) extDir(id string) string {
	return filepath.Join(s.cfg.ExtensionsDir(), id)
}
func (s *Server) stagingDir(id string) string {
	return filepath.Join(s.cfg.ExtensionsDir(), ".staging", id)
}

// freezeV1 records the DD-FREEZE moment on the first extension existing. Idempotent.
func (s *Server) freezeV1() {
	if v, _ := s.st.GetSetting("api.v1.frozen_at"); v == "" {
		_ = s.st.SetSetting("api.v1.frozen_at", strconv.FormatInt(time.Now().UnixMilli(), 10))
	}
}

// handleExtGate enables third-party extensions after a typed confirmation and sets
// the freeze (v1 becomes frozen from here). Idempotent.
func (s *Server) handleExtGate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm string `json:"confirm"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	if strings.TrimSpace(strings.ToLower(req.Confirm)) != "enable extensions" {
		writeErrorCode(w, http.StatusBadRequest, "validation", `type "enable extensions" to confirm`)
		return
	}
	_ = s.st.SetSetting("ext.enabled", "1")
	s.freezeV1()
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true})
}

// handleExtInstall stages an uploaded bundle and returns its consent metadata,
// WITHOUT committing. The client shows the consent screen, then POSTs
// /ext/install/confirm with the returned stagingId.
func (s *Server) handleExtInstall(w http.ResponseWriter, r *http.Request) {
	if !s.extEnabled() {
		writeErrorCode(w, http.StatusForbidden, "forbidden", "third-party extensions are not enabled")
		return
	}
	f, fh, err := r.FormFile("bundle")
	if err != nil {
		writeError(w, http.StatusBadRequest, "a bundle file is required (multipart field 'bundle')")
		return
	}
	defer f.Close()

	stagingID := randID()
	dir := s.stagingDir(stagingID)
	if err := unzipTo(f, fh.Size, dir); err != nil {
		_ = os.RemoveAll(dir)
		writeErrorCode(w, http.StatusBadRequest, "validation", "bad bundle: "+err.Error())
		return
	}
	m, d, _, err := loadStagedManifest(dir)
	if err != nil {
		_ = os.RemoveAll(dir)
		writeErrorCode(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	if bad := scanBundleImports(dir); len(bad) > 0 {
		_ = os.RemoveAll(dir)
		writeErrorCode(w, http.StatusBadRequest, "validation", "remote imports are not allowed: "+strings.Join(bad, ", "))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"stagingId": stagingID,
		"id":        m.ID,
		"name":      m.Name,
		"version":   m.Version,
		"author":    m.Author,
		"source":    "sideload",
		"hasBackend": d.HasBackend,
		"hasUI":      d.HasUI,
		"consent":    d.ConsentLines,
	})
}

// handleExtConfirm commits a staged bundle: moves it into place, allocates a uid,
// creates the row, and (for a backend) mints a token + spawns.
func (s *Server) handleExtConfirm(w http.ResponseWriter, r *http.Request) {
	if !s.extEnabled() {
		writeErrorCode(w, http.StatusForbidden, "forbidden", "third-party extensions are not enabled")
		return
	}
	var req struct {
		StagingID string `json:"stagingId"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	if req.StagingID == "" || strings.ContainsAny(req.StagingID, `/\.`) {
		writeErrorCode(w, http.StatusBadRequest, "validation", "invalid stagingId")
		return
	}
	dir := s.stagingDir(req.StagingID)
	m, d, raw, err := loadStagedManifest(dir)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation", "staged bundle not found or invalid")
		return
	}
	if _, e := s.st.GetExtension(m.ID); e == nil {
		_ = os.RemoveAll(dir)
		writeErrorCode(w, http.StatusConflict, "conflict", "extension "+m.ID+" is already installed")
		return
	}

	uid := s.allocExtUID()
	dest := s.extDir(m.ID)
	_ = os.RemoveAll(dest)
	if err := os.MkdirAll(filepath.Dir(dest), 0o711); err != nil {
		writeError(w, http.StatusInternalServerError, "install failed")
		return
	}
	if err := os.Rename(dir, dest); err != nil {
		_ = os.RemoveAll(dir)
		writeError(w, http.StatusInternalServerError, "install failed")
		return
	}
	chownR(dest, uid)     // hand the extension's files to its own uid (root can)
	_ = os.Chmod(dest, 0o700)

	caller := auth.FromContext(r.Context())
	ex := &store.Extension{
		ID: m.ID, Name: m.Name, Version: m.Version, Source: "sideload",
		UpdateURL: updateURL(m), ManifestJSON: string(raw),
		Scopes: d.Scopes, CameraGrants: d.CameraGrants,
		Enabled: true, ExtUID: uid, InstalledBy: caller.UserID,
	}
	if err := s.st.CreateExtension(ex); err != nil {
		writeError(w, http.StatusInternalServerError, "could not record extension")
		return
	}
	s.freezeV1()
	s.spawnIfBackend(ex)
	writeJSON(w, http.StatusCreated, s.extView(ex))
}

func (s *Server) handleExtList(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.ListExtensions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, ex := range list {
		out = append(out, s.extView(ex))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleExtEnable(w http.ResponseWriter, r *http.Request) {
	ex, err := s.st.GetExtension(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.st.SetExtensionEnabled(ex.ID, true); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	ex.Enabled = true
	s.spawnIfBackend(ex)
	writeJSON(w, http.StatusOK, s.extView(ex))
}

func (s *Server) handleExtDisable(w http.ResponseWriter, r *http.Request) {
	ex, err := s.st.GetExtension(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.st.SetExtensionEnabled(ex.ID, false); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if s.extSup != nil {
		s.extSup.Stop(ex.ID)
	}
	_ = s.st.DeleteExtensionTokens(ex.ID) // revoke; a fresh one is minted on re-enable
	ex.Enabled = false
	writeJSON(w, http.StatusOK, s.extView(ex))
}

func (s *Server) handleExtUninstall(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := s.st.GetExtension(id); err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if s.extSup != nil {
		s.extSup.Remove(id)
	}
	_ = s.st.DeleteExtensionTokens(id)
	_ = s.st.DeleteExtension(id)
	_ = os.RemoveAll(s.extDir(id)) // destroys its files + stored data
	w.WriteHeader(http.StatusNoContent)
}

// StartEnabledExtensions (re)starts every enabled backend extension at boot, and
// flags any whose on-disk files are missing (e.g. after a DB restore) instead of
// silently running stale code. Called from main after SetExtensions.
func (s *Server) StartEnabledExtensions() {
	if s.extSup == nil {
		return
	}
	list, _ := s.st.ListExtensions()
	for _, ex := range list {
		if !ex.Enabled {
			continue
		}
		if _, err := os.Stat(s.extDir(ex.ID)); err != nil {
			s.log.Warn("extension enabled but its files are missing; not starting", "id", ex.ID)
			continue
		}
		s.spawnIfBackend(ex)
	}
}

// spawnIfBackend mints a fresh scoped token and launches the extension's backend if
// it has one and is enabled. A fresh token per spawn avoids storing the plaintext
// (only the hash is persisted) and revokes any prior one.
func (s *Server) spawnIfBackend(ex *store.Extension) {
	if s.extSup == nil || !ex.Enabled {
		return
	}
	m, err := ext.Parse([]byte(ex.ManifestJSON))
	if err != nil {
		return
	}
	d, err := m.Validate()
	if err != nil || m.Backend == nil {
		return
	}
	_ = s.st.DeleteExtensionTokens(ex.ID)
	tok, err := s.st.CreateExtensionToken(ex.InstalledBy, ex.ID, ex.Name, ex.Scopes, ex.CameraGrants)
	if err != nil {
		s.log.Error("mint extension token", "id", ex.ID, "err", err)
		return
	}
	dir := s.extDir(ex.ID)
	imPath, err := ext.WriteImportMap(dir, s.sdkModule)
	if err != nil {
		s.log.Error("write extension import map", "id", ex.ID, "err", err)
		return
	}
	spec := ext.BuildSpec(ex.ID, filepath.Join(dir, m.Backend.Entry), dir, s.sdkModule, imPath, s.apiBase, tok, ex.ExtUID, d)
	s.extSup.Start(spec)
}

func (s *Server) extView(ex *store.Extension) map[string]any {
	v := map[string]any{
		"id": ex.ID, "name": ex.Name, "version": ex.Version, "source": ex.Source,
		"enabled": ex.Enabled, "scopes": ex.Scopes, "cameraGrants": ex.CameraGrants,
		"installedAt": ex.InstalledAt,
	}
	if s.extSup != nil {
		st, reason := s.extSup.Status(ex.ID)
		v["state"] = string(st)
		if reason != "" {
			v["reason"] = reason
		}
	}
	return v
}

func (s *Server) allocExtUID() int {
	max := extUIDBase - 1
	if list, err := s.st.ListExtensions(); err == nil {
		for _, e := range list {
			if e.ExtUID > max {
				max = e.ExtUID
			}
		}
	}
	return max + 1
}

// --- bundle helpers -----------------------------------------------------------

func randID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func updateURL(m *ext.Manifest) string {
	if m.Update != nil {
		return m.Update.URL
	}
	return ""
}

func loadStagedManifest(dir string) (*ext.Manifest, ext.Derived, []byte, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, ext.Derived{}, nil, errors.New("manifest.json is missing from the bundle")
	}
	m, err := ext.Parse(raw)
	if err != nil {
		return nil, ext.Derived{}, nil, err
	}
	d, err := m.Validate()
	if err != nil {
		return nil, ext.Derived{}, nil, err
	}
	return m, d, raw, nil
}

// unzipTo extracts a zip into dest with path-traversal protection and per-file and
// per-bundle size caps.
func unzipTo(ra io.ReaderAt, size int64, dest string) error {
	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return err
	}
	clean := filepath.Clean(dest)
	for _, zf := range zr.File {
		if strings.Contains(zf.Name, "..") || strings.HasPrefix(zf.Name, "/") {
			return errors.New("unsafe path in bundle: " + zf.Name)
		}
		target := filepath.Join(dest, zf.Name)
		if target != clean && !strings.HasPrefix(target, clean+string(os.PathSeparator)) {
			return errors.New("path escapes bundle: " + zf.Name)
		}
		if zf.FileInfo().IsDir() {
			_ = os.MkdirAll(target, 0o700)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			_ = rc.Close()
			return err
		}
		_, err = io.Copy(out, io.LimitReader(rc, 50<<20)) // 50 MiB per file
		_ = out.Close()
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// scanBundleImports statically rejects remote/absolute module specifiers in any
// backend source file, at install time with a clear message (R6).
func scanBundleImports(dir string) []string {
	var bad []string
	_ = filepath.WalkDir(dir, func(p string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() {
			return nil
		}
		switch filepath.Ext(p) {
		case ".ts", ".js", ".mjs", ".tsx", ".jsx":
			if b, err := os.ReadFile(p); err == nil {
				bad = append(bad, ext.CheckNoRemoteImports(string(b))...)
			}
		}
		return nil
	})
	return bad
}

func chownR(dir string, uid int) {
	_ = filepath.WalkDir(dir, func(p string, _ fs.DirEntry, err error) error {
		if err == nil {
			_ = os.Chown(p, uid, uid)
		}
		return nil
	})
}
