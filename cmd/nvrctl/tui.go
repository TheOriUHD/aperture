package main

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

func tuiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Live admin dashboard",
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			_, err = tea.NewProgram(model{c: c}, tea.WithAltScreen()).Run()
			return err
		},
	}
}

type model struct {
	c      *Client
	v      verInfo
	s      storageInfo
	cams   []cameraInfo
	evs    []eventInfo
	loaded bool
	err    error
	w, h   int
}

type dataMsg struct {
	v    verInfo
	s    storageInfo
	cams []cameraInfo
	evs  []eventInfo
	err  error
}
type tickMsg time.Time

func (m model) Init() tea.Cmd { return tea.Batch(fetch(m.c), tick()) }

func fetch(c *Client) tea.Cmd {
	return func() tea.Msg {
		var d dataMsg
		if err := c.get("/api/system/version", &d.v); err != nil {
			d.err = err
		}
		_ = c.get("/api/system/storage", &d.s)
		_ = c.get("/api/cameras", &d.cams)
		now := time.Now().UnixMilli()
		_ = c.get(fmt.Sprintf("/api/events?from=%d&to=%d&limit=8", now-7*24*3600*1000, now), &d.evs)
		return d
	}
}
func tick() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "r":
			return m, fetch(m.c)
		}
	case dataMsg:
		m.v, m.s, m.cams, m.evs, m.err, m.loaded = msg.v, msg.s, msg.cams, msg.evs, msg.err, true
	case tickMsg:
		return m, tea.Batch(fetch(m.c), tick())
	}
	return m, nil
}

func (m model) View() string {
	if !m.loaded {
		return "\n  " + stFaint.Render("Connecting to nvrd…")
	}
	if m.err != nil {
		return "\n  " + stRec.Render("error: "+m.err.Error()) + "\n  " + stFaint.Render("q quit")
	}

	online, recording := 0, 0
	for _, cam := range m.cams {
		if cam.Enabled {
			online++
		}
		if cam.Enabled && cam.RecordMode != "never" {
			recording++
		}
	}
	todayEvents := 0
	cutoff := startOfTodayMs()
	for _, e := range m.evs {
		if e.StartTS >= cutoff {
			todayEvents++
		}
	}

	title := stTitle.Render("⦿ Aperture NVR") + "  " + stFaint.Render(m.v.Version)

	cards := lipgloss.JoinHorizontal(lipgloss.Top,
		card("Cameras", fmt.Sprintf("%d/%d", online, len(m.cams))),
		card("Recording", fmt.Sprintf("%d", recording)),
		card("Storage", fmtBytes(m.s.RecordedBytes)),
		card("Free", fmtBytes(m.s.DiskFreeBytes)),
		card("Events today", fmt.Sprintf("%d", todayEvents)),
	)

	camLines := stHead.Render("CAMERAS") + "\n"
	for _, cam := range m.cams {
		mode := cam.RecordMode
		if cam.Enabled && cam.RecordMode != "never" {
			mode = stRec.Render("● ") + mode
		}
		camLines += fmt.Sprintf("  %s %-18s %s\n", dot(cam.Enabled),
			lipgloss.NewStyle().Foreground(cFg).Render(trunc(cam.Name, 18)), stFaint.Render(mode))
	}

	evLines := stHead.Render("RECENT EVENTS") + "\n"
	name := map[string]string{}
	for _, cam := range m.cams {
		name[cam.ID] = cam.Name
	}
	if len(m.evs) == 0 {
		evLines += "  " + stFaint.Render("none")
	}
	for _, e := range m.evs {
		cn := name[e.CameraID]
		if cn == "" && len(e.CameraID) >= 8 {
			cn = e.CameraID[:8]
		}
		evLines += fmt.Sprintf("  %s  %-16s %s\n", stFaint.Render(fmtTS(e.StartTS)), trunc(cn, 16), stRec.Render(e.Type))
	}

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(40).Render(camLines),
		evLines,
	)

	footer := stFaint.Render("r refresh · q quit · auto-updates every 3s")

	return "\n " + title + "\n\n " + cards + "\n\n " + body + "\n " + footer
}

func card(label, value string) string {
	inner := stFaint.Render(label) + "\n" + lipgloss.NewStyle().Bold(true).Foreground(cBrand).Render(value)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#262b33")).
		Padding(0, 2).
		MarginRight(1).
		Render(inner)
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
