package main

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	cBrand  = lipgloss.Color("#1b9df0")
	cMuted  = lipgloss.Color("#8b9199")
	cFaint  = lipgloss.Color("#6c727a")
	cOnline = lipgloss.Color("#3fce7c")
	cRec    = lipgloss.Color("#f0494e")
	cFg     = lipgloss.Color("#eef0f2")

	stTitle = lipgloss.NewStyle().Bold(true).Foreground(cBrand)
	stHead  = lipgloss.NewStyle().Bold(true).Foreground(cMuted)
	stFaint = lipgloss.NewStyle().Foreground(cFaint)
	stOK    = lipgloss.NewStyle().Foreground(cOnline)
	stRec   = lipgloss.NewStyle().Foreground(cRec)
)

func dot(on bool) string {
	if on {
		return stOK.Render("●")
	}
	return stFaint.Render("○")
}

func fmtTS(ms int64) string {
	if ms == 0 {
		return "—"
	}
	return time.UnixMilli(ms).Format("Jan 2 15:04:05")
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show a one-glance NVR status",
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			var v verInfo
			if err := c.get("/api/system/version", &v); err != nil {
				return err
			}
			var s storageInfo
			_ = c.get("/api/system/storage", &s)
			var cams []cameraInfo
			_ = c.get("/api/cameras", &cams)
			var evs []eventInfo
			_ = c.get(fmt.Sprintf("/api/events?from=%d&to=%d&limit=1000", startOfTodayMs(), time.Now().UnixMilli()), &evs)

			online, recording := 0, 0
			for _, cam := range cams {
				if cam.Enabled {
					online++
				}
				if cam.Enabled && cam.RecordMode != "never" {
					recording++
				}
			}
			fmt.Println(stTitle.Render("Aperture NVR") + "  " + stFaint.Render(v.Version+" ("+v.Commit+")"))
			fmt.Printf("  %s  %d online / %d total\n", stHead.Render("Cameras  "), online, len(cams))
			fmt.Printf("  %s  %d cameras\n", stHead.Render("Recording"), recording)
			fmt.Printf("  %s  %s recorded · %s free of %s\n", stHead.Render("Storage  "),
				fmtBytes(s.RecordedBytes), fmtBytes(s.DiskFreeBytes), fmtBytes(s.DiskTotalBytes))
			fmt.Printf("  %s  %d today\n", stHead.Render("Events   "), len(evs))
			return nil
		},
	}
}

func camerasCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cameras",
		Short: "List cameras",
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			var cams []cameraInfo
			if err := c.get("/api/cameras", &cams); err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, stHead.Render("  NAME\tMODE\tSOURCE"))
			for _, cam := range cams {
				fmt.Fprintf(tw, "%s %s\t%s\t%s\n", dot(cam.Enabled),
					lipgloss.NewStyle().Foreground(cFg).Render(cam.Name), cam.RecordMode, stFaint.Render(cam.RTSPMainURL))
			}
			return tw.Flush()
		},
	}
}

func usersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "users",
		Short: "List users",
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			var users []userInfo
			if err := c.get("/api/users", &users); err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, stHead.Render("EMAIL\tNAME\tROLE\tACCESS"))
			for _, u := range users {
				access := "all"
				if u.Role != "admin" {
					access = fmt.Sprintf("%d cameras", len(u.CameraIDs))
				}
				name := u.Name
				if u.Disabled {
					name += stFaint.Render(" (disabled)")
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", u.Email, name, u.Role, access)
			}
			return tw.Flush()
		},
	}
}

func storageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "storage",
		Short: "Show storage usage",
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			var s storageInfo
			if err := c.get("/api/system/storage", &s); err != nil {
				return err
			}
			fmt.Printf("%s  %s recorded · %s free of %s\n\n", stTitle.Render("Storage"),
				fmtBytes(s.RecordedBytes), fmtBytes(s.DiskFreeBytes), fmtBytes(s.DiskTotalBytes))
			cams := s.Cameras
			sort.Slice(cams, func(i, j int) bool { return cams[i].Bytes > cams[j].Bytes })
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			for _, cam := range cams {
				fmt.Fprintf(tw, "  %s\t%s\t%s\n", cam.Name, fmtBytes(cam.Bytes), stFaint.Render(fmt.Sprintf("%d clips", cam.Count)))
			}
			return tw.Flush()
		},
	}
}

func eventsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "events",
		Short: "List recent motion events",
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			var cams []cameraInfo
			_ = c.get("/api/cameras", &cams)
			name := map[string]string{}
			for _, cam := range cams {
				name[cam.ID] = cam.Name
			}
			var evs []eventInfo
			now := time.Now().UnixMilli()
			if err := c.get(fmt.Sprintf("/api/events?from=%d&to=%d&limit=30", now-7*24*3600*1000, now), &evs); err != nil {
				return err
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, stHead.Render("WHEN\tCAMERA\tTYPE"))
			for _, e := range evs {
				cn := name[e.CameraID]
				if cn == "" {
					cn = e.CameraID[:8]
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\n", fmtTS(e.StartTS), cn, stRec.Render(e.Type))
			}
			return tw.Flush()
		},
	}
}
