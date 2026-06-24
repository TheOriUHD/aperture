package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- styles --------------------------------------------------------------

var (
	cLine  = lipgloss.Color("#262b33")
	cSelBg = lipgloss.Color("#1b9df0")
	cSelFg = lipgloss.Color("#07151f")
	cBar   = lipgloss.Color("#1b1f26")
	cInBg  = lipgloss.Color("#11141a")
	cInBgF = lipgloss.Color("#0f2030")

	stTopBar     = lipgloss.NewStyle().Background(cBar).Foreground(cFg).Bold(true)
	stFoot       = lipgloss.NewStyle().Foreground(cFaint)
	stSel        = lipgloss.NewStyle().Background(cSelBg).Foreground(cSelFg).Bold(true)
	stRow        = lipgloss.NewStyle().Foreground(cFg)
	stPanelTitle = lipgloss.NewStyle().Foreground(cBrand).Bold(true)
	stFieldLabel = lipgloss.NewStyle().Foreground(cMuted)
	stFieldFocus = lipgloss.NewStyle().Foreground(cBrand).Bold(true)
	stToast      = lipgloss.NewStyle().Foreground(cOnline)
	stCard       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cLine).Padding(1, 3)
	stInput      = lipgloss.NewStyle().Foreground(cFg).Background(cInBg).Padding(0, 1)
	stInputFocus = lipgloss.NewStyle().Foreground(cFg).Background(cInBgF).Padding(0, 1)
)

// --- messages ------------------------------------------------------------

type loginMsg struct {
	user     *userInfo
	token    string
	restored bool
	err      error
}
type camsMsg struct {
	cams []cameraInfo
	err  error
}
type usersMsg struct {
	users []userInfo
	err   error
}
type storageMsg struct {
	s    storageInfo
	cams []cameraInfo
	err  error
}
type dashMsg struct {
	cams []cameraInfo
	s    storageInfo
	evs  []eventInfo
	err  error
}
type siteMsg struct {
	name string
	err  error
}
type haMsg struct {
	ha  haInfo
	err error
}
type oidcMsg struct {
	o   oidcInfo
	err error
}
type doneMsg struct {
	toast  string
	reload screen
	err    error
}

// --- commands ------------------------------------------------------------

func restoreCmd(c *Client) tea.Cmd {
	return func() tea.Msg {
		u, err := c.Me()
		if err != nil {
			return loginMsg{err: err, restored: true}
		}
		return loginMsg{user: u, token: c.Session(), restored: true}
	}
}

func loginCmd(c *Client, email, pass string) tea.Cmd {
	return func() tea.Msg {
		u, err := c.Login(email, pass)
		if err != nil {
			return loginMsg{err: err}
		}
		return loginMsg{user: u, token: c.Session()}
	}
}

func loadDash(c *Client) tea.Cmd {
	return func() tea.Msg {
		cams, err := c.Cameras()
		if err != nil {
			return dashMsg{err: err}
		}
		s, _ := c.Storage()
		evs, _ := c.Events(8)
		return dashMsg{cams: cams, s: s, evs: evs}
	}
}
func loadCams(c *Client) tea.Cmd {
	return func() tea.Msg { x, err := c.Cameras(); return camsMsg{cams: x, err: err} }
}
func loadUsers(c *Client) tea.Cmd {
	return func() tea.Msg { x, err := c.Users(); return usersMsg{users: x, err: err} }
}
func loadStorage(c *Client) tea.Cmd {
	return func() tea.Msg {
		s, err := c.Storage()
		if err != nil {
			return storageMsg{err: err}
		}
		cams, _ := c.Cameras()
		return storageMsg{s: s, cams: cams}
	}
}
func loadSite(c *Client) tea.Cmd {
	return func() tea.Msg { n, err := c.SiteName(); return siteMsg{name: n, err: err} }
}
func loadHA(c *Client) tea.Cmd {
	return func() tea.Msg { h, err := c.HA(); return haMsg{ha: h, err: err} }
}
func loadOIDC(c *Client) tea.Cmd {
	return func() tea.Msg { o, err := c.OIDC(); return oidcMsg{o: o, err: err} }
}

func saveCameraCmd(c *Client, id string, body any) tea.Cmd {
	return func() tea.Msg {
		if err := c.SaveCamera(id, body); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{toast: "Camera saved.", reload: scCameras}
	}
}
func deleteCameraCmd(c *Client, id string) tea.Cmd {
	return func() tea.Msg {
		if err := c.DeleteCamera(id); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{toast: "Camera deleted.", reload: scCameras}
	}
}
func saveUserCmd(c *Client, id string, body any, toast string) tea.Cmd {
	return func() tea.Msg {
		if err := c.SaveUser(id, body); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{toast: toast, reload: scUsers}
	}
}
func deleteUserCmd(c *Client, id string) tea.Cmd {
	return func() tea.Msg {
		if err := c.DeleteUser(id); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{toast: "User deleted.", reload: scUsers}
	}
}
func saveSiteCmd(c *Client, name string) tea.Cmd {
	return func() tea.Msg {
		if err := c.SetSiteName(name); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{toast: "Saved.", reload: scSystem}
	}
}
func saveHACmd(c *Client, body any) tea.Cmd {
	return func() tea.Msg {
		if err := c.SetHA(body); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{toast: "Saved.", reload: scHA}
	}
}
func saveOIDCCmd(c *Client, body any) tea.Cmd {
	return func() tea.Msg {
		if err := c.SetOIDC(body); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{toast: "Saved.", reload: scOIDC}
	}
}
func purgeCmd(c *Client, from, to int64) tea.Cmd {
	return func() tea.Msg {
		pr, err := c.Purge("", from, to)
		if err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{toast: fmt.Sprintf("Deleted %d clips · freed %s", pr.DeletedSegments, fmtBytes(pr.FreedBytes)), reload: scStorage}
	}
}

// --- form model ----------------------------------------------------------

type fkind int

const (
	fText fkind = iota
	fPass
	fChoice
	fBool
)

type ffield struct {
	key      string
	label    string
	hint     string
	kind     fkind
	ti       textinput.Model
	choices  []string
	ci       int
	b        bool
	readonly bool
}

type form struct {
	id     string
	title  string
	fields []ffield
	focus  int
}

func tinput(val string, pass bool) textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Width = 44
	ti.SetValue(val)
	if pass {
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '•'
	}
	return ti
}

func ftext(key, label, val, hint string) ffield {
	return ffield{key: key, label: label, hint: hint, kind: fText, ti: tinput(val, false)}
}
func ftextRO(key, label, val, hint string) ffield {
	f := ftext(key, label, val, hint)
	f.readonly = true
	return f
}
func fpass(key, label, hint string) ffield {
	return ffield{key: key, label: label, hint: hint, kind: fPass, ti: tinput("", true)}
}
func fchoice(key, label string, choices []string, sel int, hint string) ffield {
	if sel < 0 {
		sel = 0
	}
	return ffield{key: key, label: label, hint: hint, kind: fChoice, choices: choices, ci: sel}
}
func fbool(key, label string, val bool, hint string) ffield {
	return ffield{key: key, label: label, hint: hint, kind: fBool, b: val}
}

func (f *form) val(key string) string {
	for i := range f.fields {
		fl := &f.fields[i]
		if fl.key != key {
			continue
		}
		switch fl.kind {
		case fText, fPass:
			return fl.ti.Value()
		case fChoice:
			return fl.choices[fl.ci]
		case fBool:
			if fl.b {
				return "true"
			}
			return "false"
		}
	}
	return ""
}
func (f *form) boolVal(key string) bool { return f.val(key) == "true" }

func (a *app) activateForm() {
	a.formActive = true
	a.formErr = ""
	a.toast = ""
	a.form.focus = 0
	a.setFormFocus(0)
}

func (a *app) setFormFocus(i int) {
	a.form.focus = i
	for j := range a.form.fields {
		fl := &a.form.fields[j]
		if fl.kind != fText && fl.kind != fPass {
			continue
		}
		if j == i && !fl.readonly {
			fl.ti.Focus()
		} else {
			fl.ti.Blur()
		}
	}
}

func (a *app) moveFormFocus(d int) {
	n := len(a.form.fields)
	if n == 0 {
		return
	}
	a.form.focus = (a.form.focus + d + n) % n
	a.setFormFocus(a.form.focus)
}

func (a *app) formParent() screen {
	switch a.screen {
	case scCameraForm:
		return scCameras
	case scUserForm:
		return scUsers
	}
	return scMenu
}

func (a *app) formKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		return a.enter(a.formParent())
	case "ctrl+s":
		return a.submitForm()
	case "tab", "down":
		a.moveFormFocus(1)
		return nil
	case "shift+tab", "up":
		a.moveFormFocus(-1)
		return nil
	case "enter":
		if a.form.focus >= len(a.form.fields)-1 {
			return a.submitForm()
		}
		a.moveFormFocus(1)
		return nil
	case "left":
		fl := &a.form.fields[a.form.focus]
		if fl.kind == fChoice {
			fl.ci = (fl.ci - 1 + len(fl.choices)) % len(fl.choices)
			return nil
		}
		if fl.kind == fBool {
			fl.b = !fl.b
			return nil
		}
	case "right":
		fl := &a.form.fields[a.form.focus]
		if fl.kind == fChoice {
			fl.ci = (fl.ci + 1) % len(fl.choices)
			return nil
		}
		if fl.kind == fBool {
			fl.b = !fl.b
			return nil
		}
	case " ":
		fl := &a.form.fields[a.form.focus]
		if fl.kind == fBool {
			fl.b = !fl.b
			return nil
		}
		if fl.kind == fChoice {
			fl.ci = (fl.ci + 1) % len(fl.choices)
			return nil
		}
	}
	if a.form.fields[a.form.focus].readonly {
		return nil
	}
	return a.forwardInput(msg)
}

func (a *app) submitForm() tea.Cmd {
	f := &a.form
	switch a.screen {
	case scCameraForm:
		name := strings.TrimSpace(f.val("name"))
		main := strings.TrimSpace(f.val("rtspMainUrl"))
		if name == "" || main == "" {
			a.formErr = "Name and main RTSP URL are required."
			return nil
		}
		body := map[string]any{
			"name":            name,
			"rtspMainUrl":     main,
			"rtspSubUrl":      f.val("rtspSubUrl"),
			"recordMode":      f.val("recordMode"),
			"enabled":         f.boolVal("enabled"),
			"retentionDays":   atoiDef(f.val("retentionDays"), 7),
			"motionThreshold": atofDef(f.val("motionThreshold"), 0.02),
			"fps":             atoiDef(f.val("fps"), 0),
		}
		a.busy = true
		return tea.Batch(a.spin.Tick, saveCameraCmd(a.c, f.id, body))
	case scUserForm:
		name := strings.TrimSpace(f.val("name"))
		role := f.val("role")
		pass := f.val("password")
		if f.id == "" {
			email := strings.TrimSpace(f.val("email"))
			if email == "" || len(pass) < 8 {
				a.formErr = "Email and a password of 8+ characters are required."
				return nil
			}
			body := map[string]any{"email": email, "name": name, "role": role, "password": pass, "cameraIds": []string{}}
			a.busy = true
			return tea.Batch(a.spin.Tick, saveUserCmd(a.c, "", body, "User created."))
		}
		body := map[string]any{"name": name, "role": role, "disabled": f.boolVal("disabled")}
		if pass != "" {
			if len(pass) < 8 {
				a.formErr = "Password must be at least 8 characters."
				return nil
			}
			body["password"] = pass
		}
		a.busy = true
		return tea.Batch(a.spin.Tick, saveUserCmd(a.c, f.id, body, "User saved."))
	case scSystem:
		a.busy = true
		return tea.Batch(a.spin.Tick, saveSiteCmd(a.c, strings.TrimSpace(f.val("siteName"))))
	case scHA:
		body := map[string]any{
			"enabled":  f.boolVal("enabled"),
			"broker":   strings.TrimSpace(f.val("broker")),
			"username": f.val("username"),
			"prefix":   strings.TrimSpace(f.val("prefix")),
		}
		if p := f.val("password"); p != "" {
			body["password"] = p
		}
		a.busy = true
		return tea.Batch(a.spin.Tick, saveHACmd(a.c, body))
	case scOIDC:
		body := map[string]any{
			"enabled":    f.boolVal("enabled"),
			"name":       f.val("name"),
			"issuer":     strings.TrimSpace(f.val("issuer")),
			"clientId":   strings.TrimSpace(f.val("clientId")),
			"domain":     strings.TrimSpace(f.val("domain")),
			"autoCreate": f.boolVal("autoCreate"),
		}
		if s := f.val("clientSecret"); s != "" {
			body["clientSecret"] = s
		}
		a.busy = true
		return tea.Batch(a.spin.Tick, saveOIDCCmd(a.c, body))
	}
	return nil
}

// --- form builders -------------------------------------------------------

func (a *app) buildCameraForm(cam *cameraInfo) {
	a.screen = scCameraForm
	modes := []string{"continuous", "motion", "scheduled", "never"}
	name, main, sub, mode := "", "", "", "continuous"
	en := true
	ret, thr, fps, id, title := "7", "0.02", "0", "", "New camera"
	if cam != nil {
		id, name, main, sub, mode, en = cam.ID, cam.Name, cam.RTSPMainURL, cam.RTSPSubURL, cam.RecordMode, cam.Enabled
		ret = strconv.Itoa(cam.RetentionDays)
		thr = strconv.FormatFloat(cam.MotionThreshold, 'f', -1, 64)
		fps = strconv.Itoa(cam.FPS)
		title = "Edit camera"
	}
	a.form = form{id: id, title: title, fields: []ffield{
		ftext("name", "Name", name, ""),
		ftext("rtspMainUrl", "Main RTSP URL", main, "rtsp://user:pass@host:554/stream"),
		ftext("rtspSubUrl", "Sub-stream URL", sub, "optional; used for motion if set"),
		fchoice("recordMode", "Record mode", modes, indexOf(modes, mode), ""),
		fbool("enabled", "Enabled", en, ""),
		ftext("retentionDays", "Retention (days)", ret, ""),
		ftext("motionThreshold", "Motion sensitivity", thr, "scene-change 0–1; lower = more sensitive"),
		ftext("fps", "Capture FPS", fps, "0 = source rate"),
	}}
	a.activateForm()
}

func (a *app) buildUserForm(u *userInfo) {
	a.screen = scUserForm
	roles := []string{"viewer", "admin"}
	if u == nil {
		a.form = form{title: "New user", fields: []ffield{
			ftext("email", "Email", "", ""),
			ftext("name", "Name", "", ""),
			fchoice("role", "Role", roles, 0, ""),
			fpass("password", "Password", "min 8 characters"),
		}}
		a.activateForm()
		return
	}
	a.form = form{id: u.ID, title: "Edit user", fields: []ffield{
		ftextRO("email", "Email", u.Email, "cannot be changed"),
		ftext("name", "Name", u.Name, ""),
		fchoice("role", "Role", roles, indexOf(roles, u.Role), ""),
		fpass("password", "Password", "leave blank to keep"),
		fbool("disabled", "Disabled", u.Disabled, ""),
	}}
	a.activateForm()
}

func (a *app) buildSystemForm(siteName string) {
	a.screen = scSystem
	a.form = form{title: "System", fields: []ffield{
		ftext("siteName", "Site name", siteName, "shown across the apps and to Home Assistant"),
	}}
	a.activateForm()
}

func (a *app) buildHAForm(h haInfo) {
	a.screen = scHA
	prefix := h.Prefix
	if prefix == "" {
		prefix = "homeassistant"
	}
	hint := "MQTT password"
	if h.HasPassword {
		hint = "leave blank to keep"
	}
	a.form = form{title: "Home Assistant", fields: []ffield{
		fbool("enabled", "Publish to MQTT", h.Enabled, ""),
		ftext("broker", "Broker URL", h.Broker, "tcp://host:1883"),
		ftext("username", "Username", h.Username, ""),
		fpass("password", "Password", hint),
		ftext("prefix", "Discovery prefix", prefix, "usually homeassistant"),
	}}
	a.activateForm()
}

func (a *app) buildOIDCForm(o oidcInfo) {
	a.screen = scOIDC
	name := o.Name
	if name == "" {
		name = "Single sign-on"
	}
	hint := "OIDC client secret"
	if o.HasSecret {
		hint = "leave blank to keep"
	}
	a.form = form{title: "Single sign-on (OIDC)", fields: []ffield{
		fbool("enabled", "Enable SSO", o.Enabled, ""),
		ftext("name", "Button label", name, ""),
		ftext("issuer", "Issuer URL", o.Issuer, "https://idp.example.com/…"),
		ftext("clientId", "Client ID", o.ClientID, ""),
		fpass("clientSecret", "Client secret", hint),
		ftext("domain", "Restrict to email domain", o.Domain, "optional"),
		fbool("autoCreate", "Auto-create as viewers", o.AutoCreate, ""),
		ftextRO("redirectUrl", "Redirect URI", o.RedirectURL, "register this callback in your provider"),
	}}
	a.activateForm()
}

func (a *app) purgeSelected() tea.Cmd {
	now := time.Now().UnixMilli()
	var from int64
	var label string
	switch a.listSel {
	case 0:
		from, label = now-24*3600*1000, "the last 24 hours"
	case 1:
		from, label = now-7*24*3600*1000, "the last 7 days"
	case 2:
		from, label = 0, "ALL recorded footage"
	}
	a.confirm = &confirmState{
		msg:    "Permanently delete " + label + " across all cameras?",
		danger: true,
		action: func(ap *app) tea.Cmd { return purgeCmd(ap.c, from, now) },
	}
	return nil
}

// --- views ---------------------------------------------------------------

func (a *app) loadingBody(title string) string {
	return stPanelTitle.Render(title) + "\n\n  " + a.spin.View() + stFaint.Render(" loading…")
}

func (a *app) viewDashboard() string {
	if a.busy && len(a.cams) == 0 {
		return a.loadingBody("Dashboard")
	}
	online, recording := 0, 0
	for _, c := range a.cams {
		if c.Enabled {
			online++
		}
		if c.Enabled && c.RecordMode != "never" {
			recording++
		}
	}
	today, cut := 0, startOfTodayMs()
	for _, e := range a.evs {
		if e.StartTS >= cut {
			today++
		}
	}
	cards := lipgloss.JoinHorizontal(lipgloss.Top,
		card("Cameras", fmt.Sprintf("%d/%d", online, len(a.cams))),
		card("Recording", fmt.Sprintf("%d", recording)),
		card("Recorded", fmtBytes(a.storage.RecordedBytes)),
		card("Free", fmtBytes(a.storage.DiskFreeBytes)),
		card("Events today", fmt.Sprintf("%d", today)),
	)
	cam := stHead.Render("CAMERAS") + "\n"
	for _, c := range a.cams {
		cam += fmt.Sprintf("  %s %s %s\n", dot(c.Enabled), padRight(trunc(c.Name, 18), 18), stFaint.Render(c.RecordMode))
	}
	nm := map[string]string{}
	for _, c := range a.cams {
		nm[c.ID] = c.Name
	}
	ev := stHead.Render("RECENT EVENTS") + "\n"
	if len(a.evs) == 0 {
		ev += "  " + stFaint.Render("none")
	}
	for i, e := range a.evs {
		if i >= 6 {
			break
		}
		cn := nm[e.CameraID]
		if cn == "" {
			cn = "?"
		}
		ev += fmt.Sprintf("  %s  %s %s\n", stFaint.Render(fmtTS(e.StartTS)), padRight(trunc(cn, 16), 16), stRec.Render(e.Type))
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(40).Render(cam), ev)
	return stPanelTitle.Render("Dashboard") + "\n\n" + cards + "\n\n" + body
}

func (a *app) viewCameras() string {
	if a.busy {
		return a.loadingBody("Cameras")
	}
	rows := []string{a.row("＋  Add camera", a.listSel == 0, false)}
	for i, cam := range a.cams {
		glyph := "○"
		if cam.Enabled {
			glyph = "●"
		}
		label := glyph + "  " + padRight(trunc(cam.Name, 20), 22) + padRight(cam.RecordMode, 12) + trunc(cam.RTSPMainURL, 40)
		rows = append(rows, a.row(label, a.listSel == i+1, false))
	}
	if len(a.cams) == 0 {
		rows = append(rows, "  "+stFaint.Render("No cameras yet — select “Add camera”."))
	}
	return stPanelTitle.Render("Cameras") + "  " + stFaint.Render(fmt.Sprintf("(%d)", len(a.cams))) + "\n\n" + strings.Join(rows, "\n") + a.toastLine()
}

func (a *app) viewUsers() string {
	if a.busy {
		return a.loadingBody("Users & roles")
	}
	rows := []string{a.row("＋  Add user", a.listSel == 0, false)}
	for i, u := range a.users {
		tag := ""
		if u.Disabled {
			tag = "  (disabled)"
		}
		label := padRight(trunc(u.Email, 30), 32) + padRight(trunc(u.Name, 18), 20) + padRight(u.Role, 8) + tag
		rows = append(rows, a.row(label, a.listSel == i+1, false))
	}
	return stPanelTitle.Render("Users & roles") + "  " + stFaint.Render(fmt.Sprintf("(%d)", len(a.users))) + "\n\n" + strings.Join(rows, "\n") + a.toastLine()
}

func (a *app) viewStorage() string {
	if a.busy {
		return a.loadingBody("Storage")
	}
	s := a.storage
	summary := stRow.Render(fmtBytes(s.RecordedBytes)+" recorded") +
		stFaint.Render("    ·    "+fmtBytes(s.DiskFreeBytes)+" free of "+fmtBytes(s.DiskTotalBytes))
	actions := []string{
		a.row("Delete footage from the last 24 hours", a.listSel == 0, false),
		a.row("Delete footage from the last 7 days", a.listSel == 1, false),
		a.row("Delete ALL recorded footage", a.listSel == 2, false),
	}
	bd := stFaint.Render("Per camera")
	for _, c := range s.Cameras {
		bd += "\n  " + padRight(trunc(c.Name, 20), 22) + fmtBytes(c.Bytes)
	}
	return stPanelTitle.Render("Storage") + "\n\n" + summary + "\n" + strings.Join(actions, "\n") + "\n\n" + bd + a.toastLine()
}

func (a *app) viewForm() string {
	if !a.formActive {
		return a.loadingBody(a.breadcrumb())
	}
	f := &a.form
	blocks := make([]string, 0, len(f.fields))
	for i := range f.fields {
		blocks = append(blocks, a.fieldView(&f.fields[i], i == f.focus))
	}
	out := stPanelTitle.Render(f.title) + "\n\n" + strings.Join(blocks, "\n")
	if a.formErr != "" {
		out += "\n\n  " + stRec.Render("✗ "+a.formErr)
	} else if a.toast != "" {
		out += "\n\n  " + stToast.Render(a.toast)
	}
	return out
}

func (a *app) fieldView(fl *ffield, focused bool) string {
	marker, lab := "  ", stFieldLabel.Render(fl.label)
	if focused {
		marker, lab = stFieldFocus.Render("▸ "), stFieldFocus.Render(fl.label)
	}
	line1 := marker + lab
	if fl.hint != "" {
		line1 += "   " + stFaint.Render(fl.hint)
	}
	var line2 string
	switch fl.kind {
	case fText, fPass:
		box := stInput
		if focused {
			box = stInputFocus
		}
		line2 = "    " + box.Render(fl.ti.View())
	case fChoice:
		line2 = "    " + chooserView(fl.choices[fl.ci], focused)
	case fBool:
		line2 = "    " + toggleView(fl.b, focused)
	}
	return line1 + "\n" + line2
}

func chooserView(val string, focused bool) string {
	if focused {
		return stFieldFocus.Render("‹ ") + stRow.Render(val) + stFieldFocus.Render(" ›")
	}
	return stFaint.Render("‹ ") + stRow.Render(val) + stFaint.Render(" ›")
}

func toggleView(on, focused bool) string {
	mark, st := "○  off", stFaint
	if on {
		mark, st = "●  on", stOK
	}
	s := st.Render(mark)
	if focused {
		s = stFieldFocus.Render("[ ") + s + stFieldFocus.Render(" ]")
	}
	return s
}

// --- small helpers -------------------------------------------------------

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return 0
}

func atoiDef(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return def
}

func atofDef(s string, def float64) float64 {
	if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
		return f
	}
	return def
}
