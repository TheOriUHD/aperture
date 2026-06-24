package main

import (
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// runTUI launches the full-screen admin console.
func runTUI(c *Client) error {
	host, _ := os.Hostname()
	if host == "" {
		host = "local"
	}
	_, err := tea.NewProgram(newApp(c, host), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

type screen int

const (
	scLogin screen = iota
	scMenu
	scDashboard
	scCameras
	scCameraForm
	scUsers
	scUserForm
	scStorage
	scSystem
	scHA
	scOIDC
)

const topBarRows = 1 // the top bar occupies screen row 0

type menuItem struct {
	icon   string
	label  string
	target screen
	action int // 0 navigate, 1 logout, 2 quit
}

var menuItems = []menuItem{
	{"▤", "Dashboard", scDashboard, 0},
	{"◉", "Cameras", scCameras, 0},
	{"♟", "Users & roles", scUsers, 0},
	{"▦", "Storage", scStorage, 0},
	{"✦", "System", scSystem, 0},
	{"⌂", "Home Assistant", scHA, 0},
	{"⚷", "Single sign-on", scOIDC, 0},
	{"⤺", "Log out", scLogin, 1},
	{"✕", "Quit", scLogin, 2},
}

type confirmState struct {
	msg    string
	danger bool
	action func(*app) tea.Cmd
}

type app struct {
	c    *Client
	host string
	w, h int

	screen screen
	busy   bool
	spin   spinner.Model
	toast  string

	// auth
	user        *userInfo
	email, pass textinput.Model
	loginFocus  int
	loginErr    string

	// list state (menu / cameras / users / storage)
	listSel int
	listTop int

	// data
	cams    []cameraInfo
	users   []userInfo
	storage storageInfo
	evs     []eventInfo

	// form
	form       form
	formActive bool
	formErr    string

	confirm *confirmState
}

func newApp(c *Client, host string) app {
	email := textinput.New()
	email.Placeholder = "you@example.com"
	email.Prompt = ""
	email.Width = 36
	email.Focus()
	pass := textinput.New()
	pass.Placeholder = "••••••••"
	pass.Prompt = ""
	pass.Width = 36
	pass.EchoMode = textinput.EchoPassword
	pass.EchoCharacter = '•'

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(cBrand)

	return app{c: c, host: host, screen: scLogin, email: email, pass: pass, spin: sp}
}

func (a app) Init() tea.Cmd {
	cmds := []tea.Cmd{a.spin.Tick, textinput.Blink}
	if a.c.HasSession() {
		cmds = append(cmds, restoreCmd(a.c))
	}
	return tea.Batch(cmds...)
}

func (a app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := (&a).update(msg)
	return a, cmd
}

func (a *app) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.w, a.h = msg.Width, msg.Height
		return nil
	case spinner.TickMsg:
		if a.busy {
			var cmd tea.Cmd
			a.spin, cmd = a.spin.Update(msg)
			return cmd
		}
		return nil
	case loginMsg:
		return a.onLogin(msg)
	case camsMsg:
		a.busy = false
		if msg.err != nil {
			a.toast = errStr(msg.err)
		} else {
			a.cams = msg.cams
		}
		return nil
	case usersMsg:
		a.busy = false
		if msg.err != nil {
			a.toast = errStr(msg.err)
		} else {
			a.users = msg.users
		}
		return nil
	case storageMsg:
		a.busy = false
		if msg.err != nil {
			a.toast = errStr(msg.err)
		} else {
			a.storage, a.cams = msg.s, msg.cams
		}
		return nil
	case dashMsg:
		a.busy = false
		if msg.err != nil {
			a.toast = errStr(msg.err)
		} else {
			a.cams, a.storage, a.evs = msg.cams, msg.s, msg.evs
		}
		return nil
	case siteMsg:
		a.busy = false
		if msg.err != nil {
			a.toast = errStr(msg.err)
			return nil
		}
		a.buildSystemForm(msg.name)
		return nil
	case haMsg:
		a.busy = false
		if msg.err != nil {
			a.toast = errStr(msg.err)
			return nil
		}
		a.buildHAForm(msg.ha)
		return nil
	case oidcMsg:
		a.busy = false
		if msg.err != nil {
			a.toast = errStr(msg.err)
			return nil
		}
		a.buildOIDCForm(msg.o)
		return nil
	case doneMsg:
		a.busy = false
		if msg.err != nil {
			a.formErr = errStr(msg.err)
			a.toast = ""
			return nil
		}
		a.toast = msg.toast
		return a.enter(msg.reload)
	case tea.MouseMsg:
		return a.handleMouse(msg)
	case tea.KeyMsg:
		return a.handleKey(msg)
	}
	return a.forwardInput(msg)
}

func (a *app) onLogin(msg loginMsg) tea.Cmd {
	a.busy = false
	if msg.err != nil {
		if msg.restored {
			// A stale saved session — fall back to the login screen quietly.
			a.c.SetSession("")
			clearSession()
			return nil
		}
		a.loginErr = errStr(msg.err)
		return nil
	}
	a.user = msg.user
	a.loginErr = ""
	if !msg.restored {
		saveSession(a.c.BaseURL(), msg.token, msg.user.Email)
	}
	return a.enter(scMenu)
}

// enter switches to a screen and kicks off any data load it needs.
func (a *app) enter(s screen) tea.Cmd {
	a.screen = s
	a.listSel, a.listTop = 0, 0
	a.formActive = false
	a.formErr = ""
	switch s {
	case scDashboard:
		a.busy = true
		return tea.Batch(a.spin.Tick, loadDash(a.c))
	case scCameras:
		a.busy = true
		return tea.Batch(a.spin.Tick, loadCams(a.c))
	case scUsers:
		a.busy = true
		return tea.Batch(a.spin.Tick, loadUsers(a.c))
	case scStorage:
		a.busy = true
		return tea.Batch(a.spin.Tick, loadStorage(a.c))
	case scSystem:
		a.busy = true
		return tea.Batch(a.spin.Tick, loadSite(a.c))
	case scHA:
		a.busy = true
		return tea.Batch(a.spin.Tick, loadHA(a.c))
	case scOIDC:
		a.busy = true
		return tea.Batch(a.spin.Tick, loadOIDC(a.c))
	}
	return nil
}

// --- key handling --------------------------------------------------------

func (a *app) handleKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	if key == "ctrl+c" {
		return tea.Quit
	}
	if a.confirm != nil {
		switch key {
		case "y", "Y", "enter":
			cs := a.confirm
			a.confirm = nil
			a.busy = true
			return tea.Batch(a.spin.Tick, cs.action(a))
		case "n", "N", "esc":
			a.confirm = nil
		}
		return nil
	}
	if a.screen == scLogin {
		return a.loginKey(msg)
	}
	if a.formActive {
		return a.formKey(msg)
	}
	// list / info screens
	switch key {
	case "esc", "backspace":
		if a.screen == scMenu {
			return nil
		}
		return a.enter(scMenu)
	case "q":
		if a.screen == scMenu {
			return tea.Quit
		}
		return a.enter(scMenu)
	case "up", "k":
		a.moveSel(-1)
	case "down", "j":
		a.moveSel(1)
	case "home", "g":
		a.listSel, a.listTop = 0, 0
	case "end", "G":
		a.listSel = a.listLen() - 1
		a.fixScroll()
	case "enter", "l", " ":
		return a.activate()
	case "d":
		return a.deleteSelected()
	}
	return nil
}

func (a *app) loginKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		return tea.Quit
	case "tab", "down":
		a.loginFocus = (a.loginFocus + 1) % 2
		a.syncLoginFocus()
		return nil
	case "shift+tab", "up":
		a.loginFocus = (a.loginFocus + 1) % 2
		a.syncLoginFocus()
		return nil
	case "enter":
		if a.loginFocus == 0 {
			a.loginFocus = 1
			a.syncLoginFocus()
			return nil
		}
		email := strings.TrimSpace(a.email.Value())
		if email == "" || a.pass.Value() == "" {
			a.loginErr = "Enter your email and password."
			return nil
		}
		a.busy = true
		a.loginErr = ""
		return tea.Batch(a.spin.Tick, loginCmd(a.c, email, a.pass.Value()))
	}
	return a.forwardInput(msg)
}

func (a *app) syncLoginFocus() {
	if a.loginFocus == 0 {
		a.email.Focus()
		a.pass.Blur()
	} else {
		a.pass.Focus()
		a.email.Blur()
	}
}

// --- selection helpers ---------------------------------------------------

func (a *app) listLen() int {
	switch a.screen {
	case scMenu:
		return len(menuItems)
	case scCameras:
		return 1 + len(a.cams) // row 0 = add
	case scUsers:
		return 1 + len(a.users)
	case scStorage:
		return 3
	}
	return 0
}

func (a *app) moveSel(d int) {
	n := a.listLen()
	if n == 0 {
		return
	}
	a.listSel = (a.listSel + d + n) % n
	a.fixScroll()
}

func (a *app) visibleRows() int {
	v := a.h - topBarRows - 4 // title + blank + footer + margin
	if v < 3 {
		v = 3
	}
	return v
}

func (a *app) fixScroll() {
	v := a.visibleRows()
	if a.listSel < a.listTop {
		a.listTop = a.listSel
	}
	if a.listSel >= a.listTop+v {
		a.listTop = a.listSel - v + 1
	}
	if a.listTop < 0 {
		a.listTop = 0
	}
}

func (a *app) itemBaseY() int {
	if a.screen == scStorage {
		return topBarRows + 3 // title, blank, summary line above actions
	}
	return topBarRows + 2 // title, blank
}

func (a *app) activate() tea.Cmd {
	switch a.screen {
	case scMenu:
		it := menuItems[a.listSel]
		switch it.action {
		case 1: // logout
			a.c.Logout()
			clearSession()
			a.user = nil
			a.screen = scLogin
			a.pass.SetValue("")
			a.loginFocus = 0
			a.syncLoginFocus()
			a.toast = ""
			return nil
		case 2: // quit
			return tea.Quit
		}
		return a.enter(it.target)
	case scCameras:
		if a.listSel == 0 {
			a.buildCameraForm(nil)
			return nil
		}
		cam := a.cams[a.listSel-1]
		a.buildCameraForm(&cam)
		return nil
	case scUsers:
		if a.listSel == 0 {
			a.buildUserForm(nil)
			return nil
		}
		u := a.users[a.listSel-1]
		a.buildUserForm(&u)
		return nil
	case scStorage:
		return a.purgeSelected()
	}
	return nil
}

func (a *app) deleteSelected() tea.Cmd {
	switch a.screen {
	case scCameras:
		if a.listSel == 0 {
			return nil
		}
		cam := a.cams[a.listSel-1]
		a.confirm = &confirmState{
			msg:    "Delete camera “" + cam.Name + "” and stop recording it? Footage on disk is kept.",
			danger: true,
			action: func(ap *app) tea.Cmd { return deleteCameraCmd(ap.c, cam.ID) },
		}
	case scUsers:
		if a.listSel == 0 {
			return nil
		}
		u := a.users[a.listSel-1]
		if a.user != nil && u.ID == a.user.ID {
			a.toast = "You can’t delete your own account."
			return nil
		}
		a.confirm = &confirmState{
			msg:    "Delete user “" + u.Email + "”?",
			danger: true,
			action: func(ap *app) tea.Cmd { return deleteUserCmd(ap.c, u.ID) },
		}
	}
	return nil
}

// --- mouse ---------------------------------------------------------------

func (a *app) handleMouse(m tea.MouseMsg) tea.Cmd {
	if a.confirm != nil {
		return nil
	}
	switch a.screen {
	case scMenu, scCameras, scUsers, scStorage:
		switch m.Button {
		case tea.MouseButtonWheelUp:
			a.moveSel(-1)
			return nil
		case tea.MouseButtonWheelDown:
			a.moveSel(1)
			return nil
		}
		if m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonLeft {
			idx := m.Y - a.itemBaseY() + a.listTop
			if idx >= 0 && idx < a.listLen() {
				a.listSel = idx
				return a.activate()
			}
		}
	case scLogin:
		if m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonLeft {
			// Lower half focuses the password field.
			if m.Y > a.h/2 {
				a.loginFocus = 1
			} else {
				a.loginFocus = 0
			}
			a.syncLoginFocus()
		}
	default:
		if a.formActive {
			if m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonLeft {
				idx := (m.Y - (topBarRows + 2)) / 2
				if idx >= 0 && idx < len(a.form.fields) {
					a.setFormFocus(idx)
				}
			}
		}
	}
	return nil
}

// forwardInput routes an unhandled message to whichever text input is focused
// (keeps the cursor blinking and typing flowing).
func (a *app) forwardInput(msg tea.Msg) tea.Cmd {
	if a.screen == scLogin {
		var cmd tea.Cmd
		if a.loginFocus == 0 {
			a.email, cmd = a.email.Update(msg)
		} else {
			a.pass, cmd = a.pass.Update(msg)
		}
		return cmd
	}
	if a.formActive {
		f := a.form.focus
		if f >= 0 && f < len(a.form.fields) {
			fl := &a.form.fields[f]
			if fl.kind == fText || fl.kind == fPass {
				var cmd tea.Cmd
				fl.ti, cmd = fl.ti.Update(msg)
				return cmd
			}
		}
	}
	return nil
}

// --- view ----------------------------------------------------------------

func (a app) View() string {
	if a.w == 0 || a.h == 0 {
		return ""
	}
	if a.screen == scLogin {
		return a.viewLogin()
	}
	if a.confirm != nil {
		return a.compose(a.topBar(a.breadcrumb()), a.viewConfirm(), a.footer("y confirm  ·  n cancel"))
	}
	var body, help string
	switch a.screen {
	case scMenu:
		body, help = a.viewMenu(), "↑/↓ move  ·  enter open  ·  q quit"
	case scDashboard:
		body, help = a.viewDashboard(), "esc back  ·  r refresh"
	case scCameras:
		body, help = a.viewCameras(), "enter edit  ·  d delete  ·  esc back"
	case scUsers:
		body, help = a.viewUsers(), "enter edit  ·  d delete  ·  esc back"
	case scStorage:
		body, help = a.viewStorage(), "↑/↓ pick  ·  enter run  ·  esc back"
	case scSystem, scHA, scOIDC, scCameraForm, scUserForm:
		body, help = a.viewForm(), "tab move  ·  ←/→ change  ·  ctrl+s save  ·  esc back"
	}
	return a.compose(a.topBar(a.breadcrumb()), body, a.footer(help))
}

func (a *app) breadcrumb() string {
	switch a.screen {
	case scDashboard:
		return "Dashboard"
	case scCameras:
		return "Cameras"
	case scCameraForm:
		return "Cameras › Edit"
	case scUsers:
		return "Users & roles"
	case scUserForm:
		return "Users › Edit"
	case scStorage:
		return "Storage"
	case scSystem:
		return "System"
	case scHA:
		return "Home Assistant"
	case scOIDC:
		return "Single sign-on"
	}
	return "Configuration"
}

func (a *app) viewLogin() string {
	title := stPanelTitle.Render("⦿  Aperture NVR")
	sub := stFaint.Render("Admin console")
	email := loginField("Email", a.email.View(), a.loginFocus == 0)
	pass := loginField("Password", a.pass.View(), a.loginFocus == 1)
	status := stFaint.Render("tab to move  ·  enter to sign in  ·  esc to quit")
	if a.busy {
		status = a.spin.View() + stFaint.Render(" signing in…")
	} else if a.loginErr != "" {
		status = stRec.Render("✗ " + a.loginErr)
	}
	card := lipgloss.JoinVertical(lipgloss.Left, title, sub, "", email, "", pass, "", status)
	return lipgloss.Place(a.w, a.h, lipgloss.Center, lipgloss.Center, stCard.Render(card))
}

func loginField(label, value string, focused bool) string {
	lab := stFieldLabel.Render("  " + label)
	box := stInput
	if focused {
		lab = stFieldFocus.Render("▸ " + label)
		box = stInputFocus
	}
	return lab + "\n" + box.Render(value)
}

func (a *app) viewMenu() string {
	rows := make([]string, len(menuItems))
	for i, it := range menuItems {
		rows[i] = a.row(it.icon+"  "+it.label, i == a.listSel, it.action != 0)
	}
	greet := ""
	if a.user != nil {
		greet = stFaint.Render("Signed in as " + a.user.Name + " (" + a.user.Role + ")")
	}
	return stPanelTitle.Render("Configuration") + "   " + greet + "\n\n" + strings.Join(rows, "\n") + a.toastLine()
}

// row renders one selectable list row, padded so the selection bar spans width.
func (a *app) row(label string, sel, dim bool) string {
	w := a.w - 4
	if w < 20 {
		w = 20
	}
	text := "  " + label
	if sel {
		text = " ▸ " + label
		return stSel.Render(padRight(text, w))
	}
	if dim {
		return stFaint.Render(text)
	}
	return stRow.Render(text)
}

func (a *app) toastLine() string {
	if a.toast == "" {
		return ""
	}
	return "\n\n  " + stToast.Render(a.toast)
}

// --- frame ---------------------------------------------------------------

func (a *app) topBar(breadcrumb string) string {
	left := " ⦿ Aperture"
	if breadcrumb != "" {
		left += "  ›  " + breadcrumb
	}
	right := ""
	if a.user != nil {
		right = a.user.Email + " @ " + a.host + " "
	}
	if lipgloss.Width(left)+lipgloss.Width(right) > a.w {
		right = ""
	}
	gap := a.w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return stTopBar.MaxWidth(a.w).Render(left + strings.Repeat(" ", gap) + right)
}

func (a *app) footer(help string) string {
	return stFoot.MaxWidth(a.w).Render(" " + help)
}

func (a *app) compose(top, body, footer string) string {
	lines := strings.Split(body, "\n")
	avail := a.h - 2
	if avail < 1 {
		avail = 1
	}
	for len(lines) < avail {
		lines = append(lines, "")
	}
	if len(lines) > avail {
		lines = lines[:avail]
	}
	return top + "\n" + strings.Join(lines, "\n") + "\n" + footer
}

func (a *app) viewConfirm() string {
	icon := stTitle.Render("?")
	if a.confirm.danger {
		icon = stRec.Render("⚠")
	}
	box := stCard.Render(icon + "  " + a.confirm.msg + "\n\n" + stFaint.Render("y") + " confirm    " + stFaint.Render("n") + " cancel")
	return lipgloss.Place(a.w, a.h-2, lipgloss.Center, lipgloss.Center, box)
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	if ae, ok := err.(*APIError); ok {
		return ae.Msg
	}
	return err.Error()
}

func padRight(s string, w int) string {
	d := w - lipgloss.Width(s)
	if d <= 0 {
		return s
	}
	return s + strings.Repeat(" ", d)
}

// dashboard cards (reused from the read-only view).
func card(label, value string) string {
	inner := stFaint.Render(label) + "\n" + lipgloss.NewStyle().Bold(true).Foreground(cBrand).Render(value)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cLine).
		Padding(0, 2).
		MarginRight(1).
		Render(inner)
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 1 {
		return ""
	}
	return s[:n-1] + "…"
}
