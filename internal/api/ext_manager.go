package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/ext"
	"github.com/TheOriUHD/aperture/internal/store"
)

// Pass 2 extension manager: logs, pinning + the nav endpoint, install-from-URL,
// the curated store, deliberate updates with revert, and the in-app file editor.
// The install/confirm/enable/disable/uninstall primitives live in ext_handlers.go;
// this file builds the management surface on top of them.

const (
	maxBundleBytes  = 50 << 20 // a fetched bundle/index is capped like an upload
	maxFetchBytes   = 8 << 20  // a manifest / store index is small
	maxEditBytes    = 2 << 20  // a single edited source file
	fetchTimeout    = 20 * time.Second
	storeURLSetting = "ext.store_url"
)

func (s *Server) extPrevDir(id string) string { return s.extDir(id) + ".prev" }

// --- logs --------------------------------------------------------------------

func (s *Server) handleExtLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := s.st.GetExtension(id); err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	lines := []string{}
	if s.extSup != nil {
		lines = s.extSup.Logs(id)
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines})
}

// --- pinning + nav rail ------------------------------------------------------

// handleExtPin grants an extension a nav-rail entry + full page. Pinning is a USER
// decision, never manifest-asserted: the extension must DECLARE a full-page view
// (a nav-panel slot), but only the user decides it earns rail space.
func (s *Server) handleExtPin(w http.ResponseWriter, r *http.Request) {
	ex, err := s.st.GetExtension(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if !manifestHasNavPanel(ex.ManifestJSON) {
		writeErrorCode(w, http.StatusBadRequest, "validation",
			"this extension has no full-page view to pin (it must declare a nav-panel)")
		return
	}
	if err := s.st.SetExtensionPinned(ex.ID, true); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	ex.Pinned = true
	writeJSON(w, http.StatusOK, s.extView(ex))
}

func (s *Server) handleExtUnpin(w http.ResponseWriter, r *http.Request) {
	ex, err := s.st.GetExtension(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err := s.st.SetExtensionPinned(ex.ID, false); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	ex.Pinned = false
	writeJSON(w, http.StatusOK, s.extView(ex))
}

// handleExtNav lists the pinned + enabled extensions the nav rail renders (any
// authenticated user). The icon is a PATH the host loads as an <img> under the
// extension CSP — never inline markup — so a hostile bundle cannot run script in
// the host session via the rail.
func (s *Server) handleExtNav(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	if s.extEnabled() {
		list, _ := s.st.ListExtensions()
		for _, ex := range list {
			if !ex.Enabled || !ex.Pinned {
				continue
			}
			m, err := ext.Parse([]byte(ex.ManifestJSON))
			if err != nil {
				continue
			}
			d, err := m.Validate()
			if err != nil || !d.HasNavPanel {
				continue
			}
			item := map[string]any{"id": ex.ID, "name": ex.Name}
			if d.Icon != "" {
				item["icon"] = "/ext/" + ex.ID + "/" + d.Icon
			}
			out = append(out, item)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func manifestHasNavPanel(manifestJSON string) bool {
	m, err := ext.Parse([]byte(manifestJSON))
	if err != nil {
		return false
	}
	d, err := m.Validate()
	return err == nil && d.HasNavPanel
}

// --- install from URL (sideload) + the curated store -------------------------

// handleExtInstallURL fetches a bundle zip from an admin-supplied URL and stages
// it exactly like an upload (source = sideload → the consent screen shows the
// sideload warning). Admin-initiated and deliberate, so no SSRF filtering: a LAN
// update source (e.g. a homelab host) is a legitimate, common case.
func (s *Server) handleExtInstallURL(w http.ResponseWriter, r *http.Request) {
	if !s.extEnabled() {
		writeErrorCode(w, http.StatusForbidden, "forbidden", "third-party extensions are not enabled")
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	b, err := fetchBounded(req.URL, maxBundleBytes)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation", "could not fetch bundle: "+err.Error())
		return
	}
	view, err := s.stageBundleBytes(b, "sideload")
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

type storeItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Description string `json:"description"`
	BundleURL   string `json:"bundleUrl"`
}

// handleExtStore returns the curated store index (a static JSON document the owner
// hosts), annotated with which items are already installed. Returns
// configured:false when no store URL is set, so the UI can show a considered empty
// state instead of an error.
func (s *Server) handleExtStore(w http.ResponseWriter, r *http.Request) {
	storeURL, _ := s.st.GetSetting(storeURLSetting)
	if strings.TrimSpace(storeURL) == "" {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false, "extensions": []any{}})
		return
	}
	items, err := s.fetchStoreIndex(storeURL)
	if err != nil {
		writeErrorCode(w, http.StatusBadGateway, "unavailable", "could not load the store: "+err.Error())
		return
	}
	installed := map[string]bool{}
	if list, e := s.st.ListExtensions(); e == nil {
		for _, ex := range list {
			installed[ex.ID] = true
		}
	}
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		out = append(out, map[string]any{
			"id": it.ID, "name": it.Name, "version": it.Version, "author": it.Author,
			"description": it.Description, "installed": installed[it.ID],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"configured": true, "extensions": out})
}

// handleExtStoreInstall stages a store item by id (source = store → no sideload
// warning; the owner reviewed and republished it). The bundle URL is resolved
// server-side from the store index, never taken from the client.
func (s *Server) handleExtStoreInstall(w http.ResponseWriter, r *http.Request) {
	if !s.extEnabled() {
		writeErrorCode(w, http.StatusForbidden, "forbidden", "third-party extensions are not enabled")
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	storeURL, _ := s.st.GetSetting(storeURLSetting)
	if strings.TrimSpace(storeURL) == "" {
		writeErrorCode(w, http.StatusBadRequest, "validation", "no store is configured")
		return
	}
	items, err := s.fetchStoreIndex(storeURL)
	if err != nil {
		writeErrorCode(w, http.StatusBadGateway, "unavailable", "could not load the store: "+err.Error())
		return
	}
	var chosen *storeItem
	for i := range items {
		if items[i].ID == req.ID {
			chosen = &items[i]
			break
		}
	}
	if chosen == nil || chosen.BundleURL == "" {
		writeErrorCode(w, http.StatusNotFound, "not_found", "no such store item")
		return
	}
	b, err := fetchBounded(chosen.BundleURL, maxBundleBytes)
	if err != nil {
		writeErrorCode(w, http.StatusBadGateway, "unavailable", "could not fetch bundle: "+err.Error())
		return
	}
	view, err := s.stageBundleBytes(b, "store")
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) fetchStoreIndex(storeURL string) ([]storeItem, error) {
	b, err := fetchBounded(storeURL, maxFetchBytes)
	if err != nil {
		return nil, err
	}
	var idx struct {
		Extensions []storeItem `json:"extensions"`
	}
	if err := json.Unmarshal(b, &idx); err != nil {
		return nil, errors.New("store index is not valid JSON")
	}
	return idx.Extensions, nil
}

// --- updates (check / apply / revert) ----------------------------------------

// handleExtUpdateCheck fetches the extension's update.url (a manifest.json) and
// compares versions. It is a read: it never changes anything. The bundle to apply
// is the sibling bundle.zip; permission changes are surfaced so the UI can force
// fresh consent before applying.
func (s *Server) handleExtUpdateCheck(w http.ResponseWriter, r *http.Request) {
	ex, err := s.st.GetExtension(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if ex.UpdateURL == "" {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "currentVersion": ex.Version})
		return
	}
	b, err := fetchBounded(ex.UpdateURL, maxFetchBytes)
	if err != nil {
		writeErrorCode(w, http.StatusBadGateway, "unavailable", "could not check for updates: "+err.Error())
		return
	}
	m, err := ext.Parse(b)
	if err != nil {
		writeErrorCode(w, http.StatusBadGateway, "unavailable", "update manifest is invalid: "+err.Error())
		return
	}
	d, err := m.Validate()
	if err != nil {
		writeErrorCode(w, http.StatusBadGateway, "unavailable", "update manifest is invalid: "+err.Error())
		return
	}
	resp := map[string]any{
		"available":      semverNewer(m.Version, ex.Version),
		"currentVersion": ex.Version,
		"latestVersion":  m.Version,
	}
	if resp["available"].(bool) {
		resp["bundleUrl"] = siblingURL(ex.UpdateURL, "bundle.zip")
		resp["consent"] = d.ConsentLines
		resp["permissionsChanged"] = permsExpanded(d, ex)
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleExtUpdateApply applies a staged newer bundle over an installed extension:
// it verifies identity, enforces fresh consent when permissions expand, backs up
// the current install for revert, swaps in the new files, re-mints the token, and
// respawns. Apply is always deliberate (never silent).
func (s *Server) handleExtUpdateApply(w http.ResponseWriter, r *http.Request) {
	ex, err := s.st.GetExtension(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var req struct {
		StagingID string `json:"stagingId"`
		Consented bool   `json:"consented"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	if req.StagingID == "" || strings.ContainsAny(req.StagingID, `/\.`) {
		writeErrorCode(w, http.StatusBadRequest, "validation", "invalid stagingId")
		return
	}
	sdir := s.stagingDir(req.StagingID)
	m, d, raw, err := loadStagedManifest(sdir)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation", "staged bundle not found or invalid")
		return
	}
	if m.ID != ex.ID {
		_ = os.RemoveAll(sdir)
		writeErrorCode(w, http.StatusBadRequest, "validation",
			fmt.Sprintf("this bundle is %s, not %s", m.ID, ex.ID))
		return
	}
	if permsExpanded(d, ex) && !req.Consented {
		writeErrorCode(w, http.StatusConflict, "consent_required",
			"this update requests additional permissions; re-consent is required")
		return
	}

	// Stop, back up the current install, swap in the new files.
	if s.extSup != nil {
		s.extSup.Stop(ex.ID)
	}
	prev := s.extPrevDir(ex.ID)
	_ = os.RemoveAll(prev)
	dest := s.extDir(ex.ID)
	if err := os.Rename(dest, prev); err != nil {
		writeError(w, http.StatusInternalServerError, "update failed (could not snapshot current version)")
		return
	}
	if err := os.Rename(sdir, dest); err != nil {
		_ = os.Rename(prev, dest) // roll back to the snapshot
		s.spawnIfBackend(ex)
		writeError(w, http.StatusInternalServerError, "update failed (could not install new version)")
		return
	}
	chownR(dest, ex.ExtUID)
	_ = os.Chmod(dest, 0o700)

	ex.Name, ex.Version, ex.UpdateURL, ex.ManifestJSON = m.Name, m.Version, updateURL(m), string(raw)
	ex.Scopes, ex.CameraGrants = d.Scopes, d.CameraGrants
	if err := s.st.UpdateExtension(ex); err != nil {
		s.log.Error("update: persist row", "id", ex.ID, "err", err)
	}
	_ = s.st.SetExtensionModified(ex.ID, false) // fresh clean bundle
	ex.Modified = false
	s.spawnIfBackend(ex) // re-mints the token with the new scopes
	writeJSON(w, http.StatusOK, s.extView(ex))
}

// handleExtRevert restores the pre-update snapshot kept by the last apply.
func (s *Server) handleExtRevert(w http.ResponseWriter, r *http.Request) {
	ex, err := s.st.GetExtension(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	prev := s.extPrevDir(ex.ID)
	if _, err := os.Stat(prev); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation", "there is no previous version to revert to")
		return
	}
	raw, err := os.ReadFile(filepath.Join(prev, "manifest.json"))
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation", "the previous version is unreadable")
		return
	}
	m, err := ext.Parse(raw)
	if err != nil {
		writeErrorCode(w, http.StatusBadRequest, "validation", "the previous version is invalid")
		return
	}
	d, _ := m.Validate()

	if s.extSup != nil {
		s.extSup.Stop(ex.ID)
	}
	dest := s.extDir(ex.ID)
	discard := dest + ".discard"
	_ = os.RemoveAll(discard)
	if err := os.Rename(dest, discard); err != nil {
		writeError(w, http.StatusInternalServerError, "revert failed")
		return
	}
	if err := os.Rename(prev, dest); err != nil {
		_ = os.Rename(discard, dest)
		s.spawnIfBackend(ex)
		writeError(w, http.StatusInternalServerError, "revert failed")
		return
	}
	_ = os.RemoveAll(discard)
	chownR(dest, ex.ExtUID)
	_ = os.Chmod(dest, 0o700)

	ex.Name, ex.Version, ex.UpdateURL, ex.ManifestJSON = m.Name, m.Version, updateURL(m), string(raw)
	ex.Scopes, ex.CameraGrants = d.Scopes, d.CameraGrants
	if err := s.st.UpdateExtension(ex); err != nil {
		s.log.Error("revert: persist row", "id", ex.ID, "err", err)
	}
	_ = s.st.SetExtensionModified(ex.ID, false)
	ex.Modified = false
	s.spawnIfBackend(ex)
	writeJSON(w, http.StatusOK, s.extView(ex))
}

// --- in-app file editor (T2.3) -----------------------------------------------

type extFileEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// handleExtFiles returns the editable file tree (flat, forward-slash paths). It
// omits managed/runtime artefacts the user shouldn't edit.
func (s *Server) handleExtFiles(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := s.st.GetExtension(id); err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	base := filepath.Clean(s.extDir(id))
	var files []extFileEntry
	_ = filepath.WalkDir(base, func(p string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(base, p)
		rel = filepath.ToSlash(rel)
		if isManagedFile(rel) {
			return nil
		}
		fi, e := de.Info()
		if e != nil {
			return nil
		}
		files = append(files, extFileEntry{Path: rel, Size: fi.Size()})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

func (s *Server) handleExtFileRead(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := s.st.GetExtension(id); err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	full, rel, ok := s.resolveExtFile(id, chi.URLParam(r, "*"))
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	fi, err := os.Stat(full)
	if err != nil || fi.IsDir() {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if fi.Size() > maxEditBytes {
		writeErrorCode(w, http.StatusRequestEntityTooLarge, "payload_too_large", "file is too large to edit in-app")
		return
	}
	b, err := os.ReadFile(full)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read file")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": rel, "content": string(b), "binary": !utf8Text(b)})
}

// handleExtFileWrite saves an edited file, re-chowns it to the extension's uid,
// marks the extension modified, and respawns its backend so the edit takes effect.
func (s *Server) handleExtFileWrite(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ex, err := s.st.GetExtension(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	full, _, ok := s.resolveExtFile(id, chi.URLParam(r, "*"))
	if !ok {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	if int64(len(req.Content)) > maxEditBytes {
		writeErrorCode(w, http.StatusRequestEntityTooLarge, "payload_too_large", "file is too large")
		return
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save")
		return
	}
	if err := os.WriteFile(full, []byte(req.Content), 0o600); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save")
		return
	}
	_ = os.Chown(full, ex.ExtUID, ex.ExtUID)
	_ = s.st.SetExtensionModified(ex.ID, true)
	ex.Modified = true
	if ex.Enabled {
		s.spawnIfBackend(ex) // restart the backend so the edit takes effect
	}
	writeJSON(w, http.StatusOK, s.extView(ex))
}

// resolveExtFile maps a client path to an absolute path inside the extension dir,
// rejecting traversal and the managed runtime files. Returns (abs, cleanRel, ok).
func (s *Server) resolveExtFile(id, raw string) (string, string, bool) {
	clean := path.Clean("/" + raw)
	if clean == "/" || isManagedFile(strings.TrimPrefix(clean, "/")) {
		return "", "", false
	}
	base := filepath.Clean(s.extDir(id))
	full := filepath.Join(base, filepath.FromSlash(clean))
	if full != base && !strings.HasPrefix(full, base+string(os.PathSeparator)) {
		return "", "", false
	}
	return full, strings.TrimPrefix(clean, "/"), true
}

// isManagedFile hides runtime/managed artefacts from the editor: the generated
// import map, the Deno cache, and any dependency dump.
func isManagedFile(rel string) bool {
	return rel == ".import-map.json" ||
		rel == ".deno" || strings.HasPrefix(rel, ".deno/") ||
		strings.HasPrefix(rel, "node_modules/")
}

// --- shared helpers ----------------------------------------------------------

// stageBundleBytes unzips a bundle into a fresh staging dir, validates its
// manifest (+ icon + no remote imports), records its source, and returns the
// consent view the confirm step later commits.
func (s *Server) stageBundleBytes(b []byte, source string) (map[string]any, error) {
	stagingID := randID()
	dir := s.stagingDir(stagingID)
	if err := unzipTo(bytes.NewReader(b), int64(len(b)), dir); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("bad bundle: %w", err)
	}
	m, d, _, err := loadStagedManifest(dir)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	if bad := scanBundleImports(dir); len(bad) > 0 {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("remote imports are not allowed: %s", strings.Join(bad, ", "))
	}
	s.writeStageSource(stagingID, source)
	return stageConsentView(stagingID, m, d, source), nil
}

func stageConsentView(stagingID string, m *ext.Manifest, d ext.Derived, source string) map[string]any {
	return map[string]any{
		"stagingId": stagingID, "id": m.ID, "name": m.Name, "version": m.Version,
		"author": m.Author, "description": m.Description, "source": source,
		"hasBackend": d.HasBackend, "hasUI": d.HasUI, "consent": d.ConsentLines,
	}
}

// The source (store|sideload) is remembered in a sibling marker file, so confirm
// can trust it without the client asserting provenance.
func (s *Server) stageSourcePath(id string) string { return s.stagingDir(id) + ".src" }
func (s *Server) writeStageSource(id, source string) {
	_ = os.WriteFile(s.stageSourcePath(id), []byte(source), 0o600)
}
func (s *Server) readStageSource(id string) string {
	b, err := os.ReadFile(s.stageSourcePath(id))
	if err != nil || (string(b) != "store" && string(b) != "sideload") {
		return "sideload"
	}
	return string(b)
}
func (s *Server) clearStageSource(id string) { _ = os.Remove(s.stageSourcePath(id)) }

// permsExpanded reports whether a staged/updated manifest asks for any scope,
// camera, or network host the installed extension was not already granted.
func permsExpanded(d ext.Derived, ex *store.Extension) bool {
	if !subset(d.Scopes, ex.Scopes) {
		return true
	}
	// Empty camera grants = "all owner cameras". Widening from a specific set to
	// all cameras, or adding ids to a scoped set, is an expansion; narrowing is not.
	if len(ex.CameraGrants) > 0 && (len(d.CameraGrants) == 0 || !subset(d.CameraGrants, ex.CameraGrants)) {
		return true
	}
	return false
}

func subset(a, b []string) bool {
	set := make(map[string]bool, len(b))
	for _, x := range b {
		set[x] = true
	}
	for _, x := range a {
		if !set[x] {
			return false
		}
	}
	return true
}

func semverNewer(a, b string) bool {
	pa, pb := parseSemver(a), parseSemver(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parseSemver(v string) [3]int {
	v = strings.SplitN(strings.TrimSpace(v), "-", 2)[0]
	var out [3]int
	for i, p := range strings.SplitN(v, ".", 3) {
		if i > 2 {
			break
		}
		out[i], _ = strconv.Atoi(strings.TrimSpace(p))
	}
	return out
}

// siblingURL replaces the last path segment of a URL with name (e.g. the
// bundle.zip that sits next to a manifest.json).
func siblingURL(raw, name string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.Path = path.Join(path.Dir(u.Path), name)
	u.RawQuery = ""
	return u.String()
}

func fetchBounded(rawURL string, max int64) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("must be an absolute http(s) URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("response exceeds the %d-byte limit", max)
	}
	return b, nil
}

// utf8Text is a cheap "is this editable text" check: reject NUL bytes.
func utf8Text(b []byte) bool { return !bytes.Contains(b, []byte{0}) }
