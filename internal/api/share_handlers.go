package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/TheOriUHD/aperture/internal/auth"
	"github.com/TheOriUHD/aperture/internal/store"
)

func randToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func shareExpired(sh *store.Share) bool {
	return sh.ExpiresAt > 0 && time.Now().Unix() > sh.ExpiresAt
}

// --- authenticated: manage shares ---------------------------------------

func (s *Server) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ExportID     string `json:"exportId"`
		Label        string `json:"label"`
		ExpiresHours int    `json:"expiresHours"`
		Password     string `json:"password"`
		MaxViews     int    `json:"maxViews"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	job, err := s.st.GetExport(req.ExportID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.isOwnerOrAdmin(r, job.UserID) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	if job.Status != "done" {
		writeError(w, http.StatusConflict, "clip is not ready to share yet")
		return
	}
	c := auth.FromContext(r.Context())
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = job.CameraName + " clip"
	}
	token := randToken()
	sh := &store.Share{
		TokenHash: sha256hex(token),
		Kind:      "clip",
		Target:    job.ID,
		Label:     label,
		MaxViews:  req.MaxViews,
		CreatedBy: c.UserID,
		CreatedAt: time.Now().Unix(),
	}
	if req.ExpiresHours > 0 {
		sh.ExpiresAt = time.Now().Add(time.Duration(req.ExpiresHours) * time.Hour).Unix()
	}
	if req.Password != "" {
		if h, err := auth.HashPassword(req.Password); err == nil {
			sh.PasswordHash = h
		}
	}
	if err := s.st.CreateShare(sh); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create share")
		return
	}
	s.logAudit(r, "share.create", label, job.CameraName)
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":        sh.ID,
		"label":     sh.Label,
		"url":       strings.TrimRight(s.cfg.PublicURL, "/") + "/s/" + token,
		"expiresAt": sh.ExpiresAt,
		"maxViews":  sh.MaxViews,
	})
}

func (s *Server) handleListShares(w http.ResponseWriter, r *http.Request) {
	c := auth.FromContext(r.Context())
	shares, err := s.st.ListSharesByUser(c.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if shares == nil {
		shares = []*store.Share{}
	}
	out := make([]map[string]any, 0, len(shares))
	for _, sh := range shares {
		out = append(out, map[string]any{
			"id":          sh.ID,
			"label":       sh.Label,
			"expiresAt":   sh.ExpiresAt,
			"maxViews":    sh.MaxViews,
			"views":       sh.Views,
			"revoked":     sh.Revoked,
			"hasPassword": sh.PasswordHash != "",
			"createdAt":   sh.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteShare(w http.ResponseWriter, r *http.Request) {
	sh, err := s.st.GetShare(chi.URLParam(r, "id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !s.isOwnerOrAdmin(r, sh.CreatedBy) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	_ = s.st.DeleteShare(sh.ID)
	s.logAudit(r, "share.revoke", sh.Label, "")
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// --- public: the chrome-less viewer -------------------------------------

func (s *Server) lookupShare(token string) (*store.Share, bool) {
	sh, err := s.st.GetShareByTokenHash(sha256hex(token))
	if err != nil || sh.Revoked || shareExpired(sh) {
		return nil, false
	}
	return sh, true
}

func (s *Server) handleSharePage(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	sh, ok := s.lookupShare(token)
	if !ok {
		renderShare(w, http.StatusNotFound, sharePage{Error: "This link is no longer available."})
		return
	}
	if sh.MaxViews > 0 && sh.Views >= sh.MaxViews {
		renderShare(w, http.StatusGone, sharePage{Error: "This link has reached its view limit."})
		return
	}
	pw := r.URL.Query().Get("pw")
	if sh.PasswordHash != "" {
		if pw == "" || !auth.VerifyPassword(sh.PasswordHash, pw) {
			renderShare(w, http.StatusOK, sharePage{
				Token:     token,
				Label:     sh.Label,
				NeedsPass: true,
				BadPass:   pw != "",
			})
			return
		}
	}
	_ = s.st.IncShareViews(sh.ID)
	s.logAuditAs(r, "public", "share.access", sh.Label, "")
	media := "/s/" + token + "/media"
	if pw != "" {
		media += "?pw=" + url.QueryEscape(pw)
	}
	renderShare(w, http.StatusOK, sharePage{
		Label:   sh.Label,
		Media:   media,
		Expires: sh.ExpiresAt,
	})
}

func (s *Server) handleShareMedia(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	sh, ok := s.lookupShare(token)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if sh.PasswordHash != "" {
		if pw := r.URL.Query().Get("pw"); pw == "" || !auth.VerifyPassword(sh.PasswordHash, pw) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
	}
	job, err := s.st.GetExport(sh.Target)
	if err != nil || job.Status != "done" || job.OutputPath == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	f, err := os.Open(job.OutputPath)
	if err != nil {
		http.Error(w, "gone", http.StatusGone)
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeContent(w, r, "clip.mp4", fi.ModTime(), f)
}

type sharePage struct {
	Token     string
	Label     string
	Media     string
	Error     string
	NeedsPass bool
	BadPass   bool
	Expires   int64
}

func renderShare(w http.ResponseWriter, code int, p sharePage) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_ = shareTmpl.Execute(w, p)
}

var shareTmpl = template.Must(template.New("share").Parse(`<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex,nofollow">
<title>Aperture — Shared clip</title>
<style>
:root{color-scheme:dark}
*{box-sizing:border-box}
body{margin:0;min-height:100vh;background:#0d0f13;color:#eef0f2;
  font-family:Inter,system-ui,-apple-system,Segoe UI,Roboto,sans-serif;
  display:flex;flex-direction:column;align-items:center;justify-content:center;gap:18px;padding:24px}
.brand{display:flex;align-items:center;gap:8px;font-weight:600;letter-spacing:.2px;color:#8b9199}
.dot{width:10px;height:10px;border-radius:50%;background:#1b9df0;box-shadow:0 0 12px #1b9df0}
h1{font-size:15px;font-weight:500;margin:0;color:#eef0f2}
video{max-width:min(960px,94vw);width:100%;border-radius:10px;background:#000;
  border:1px solid #20242b;box-shadow:0 12px 40px rgba(0,0,0,.5)}
.card{background:#14171c;border:1px solid #20242b;border-radius:12px;padding:28px 26px;max-width:380px;width:100%}
.muted{color:#6c727a;font-size:12px;text-align:center}
input{width:100%;height:40px;border-radius:8px;background:#0d0f13;border:1px solid #262b33;
  color:#eef0f2;padding:0 12px;font-size:14px;margin-top:6px}
button{width:100%;height:40px;border-radius:8px;border:0;background:#1b9df0;color:#06121f;
  font-weight:600;font-size:14px;margin-top:12px;cursor:pointer}
label{font-size:12px;color:#8b9199}
.err{color:#f0494e;font-size:12px;margin-top:8px}
.foot{color:#6c727a;font-size:11px}
</style></head><body>
<div class="brand"><span class="dot"></span> Aperture</div>
{{if .Error}}
  <h1>{{.Error}}</h1>
{{else if .NeedsPass}}
  <form class="card" method="get" action="/s/{{.Token}}">
    <h1 style="margin-bottom:14px">{{.Label}}</h1>
    <label>This clip is password protected</label>
    <input type="password" name="pw" placeholder="Password" autofocus>
    {{if .BadPass}}<div class="err">Incorrect password.</div>{{end}}
    <button type="submit">View clip</button>
  </form>
{{else}}
  <h1>{{.Label}}</h1>
  <video controls autoplay playsinline preload="metadata" src="{{.Media}}"></video>
  <div class="foot">Shared securely via Aperture NVR</div>
{{end}}
</body></html>`))
