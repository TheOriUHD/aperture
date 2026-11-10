package api

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/TheOriUHD/aperture/internal/hwinfo"
)

// The hardware profile is what an admin confirms the machine is: the CPU (picked
// from the web app's catalogue, so it stays meaningful inside a VM whose CPU
// reads "QEMU Virtual CPU"), how many CPU threads and how much memory Aperture may
// use, the network link, and an optional AI accelerator. The web app turns it
// into the dashboard's camera capacity and the soft limit on adding cameras; the
// server only stores it.
const hardwareProfileKey = "hardware_profile"

type hardwareAccelerator struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type hardwareProfile struct {
	CPUID       string               `json:"cpuId"`
	CPUName     string               `json:"cpuName"`
	Threads     int                  `json:"threads"`
	RAMGB       float64              `json:"ramGb"`
	NICGbps     float64              `json:"nicGbps"`
	Accelerator *hardwareAccelerator `json:"accelerator"`
	UpdatedAt   int64                `json:"updatedAt"`
}

var catalogID = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]{0,79}$`)

// problem words what is wrong with a submitted profile, or "".
func (p *hardwareProfile) problem() string {
	if !catalogID.MatchString(p.CPUID) {
		return "pick a CPU from the list"
	}
	if strings.TrimSpace(p.CPUName) == "" || len(p.CPUName) > 120 {
		return "the CPU name is missing or too long"
	}
	if p.Threads < 1 || p.Threads > 2048 {
		return "CPU threads must be between 1 and 2048"
	}
	if math.IsNaN(p.RAMGB) || p.RAMGB < 0.25 || p.RAMGB > 16384 {
		return "memory must be between 0.25 and 16384 GB"
	}
	if math.IsNaN(p.NICGbps) || p.NICGbps < 0.01 || p.NICGbps > 800 {
		return "network speed must be between 0.01 and 800 Gbit/s"
	}
	if a := p.Accelerator; a != nil {
		if !catalogID.MatchString(a.ID) {
			return "pick an accelerator from the list"
		}
		if strings.TrimSpace(a.Name) == "" || len(a.Name) > 120 {
			return "the accelerator name is missing or too long"
		}
	}
	return ""
}

func (s *Server) hardwareProfile() *hardwareProfile {
	raw, err := s.st.GetSetting(hardwareProfileKey)
	if err != nil || raw == "" {
		return nil
	}
	var p hardwareProfile
	if json.Unmarshal([]byte(raw), &p) != nil || p.problem() != "" {
		return nil
	}
	return &p
}

// handleGetHardware returns the saved profile (null until an admin sets one) and
// what the server detects about itself, which seeds the form.
func (s *Server) handleGetHardware(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"profile":  s.hardwareProfile(),
		"detected": hwinfo.Detect("/"),
	})
}

func (s *Server) handleSetHardware(w http.ResponseWriter, r *http.Request) {
	var p hardwareProfile
	if err := decodeJSON(r, &p); err != nil {
		writeError(w, http.StatusBadRequest, decodeErrMsg(err))
		return
	}
	p.CPUName = strings.TrimSpace(p.CPUName)
	if p.Accelerator != nil {
		p.Accelerator.Name = strings.TrimSpace(p.Accelerator.Name)
	}
	if msg := p.problem(); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	p.UpdatedAt = time.Now().UnixMilli()
	raw, _ := json.Marshal(p)
	if err := s.st.SetSetting(hardwareProfileKey, string(raw)); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	accel := "none"
	if p.Accelerator != nil {
		accel = p.Accelerator.Name
	}
	s.logAudit(r, "settings.hardware", "hardware profile",
		fmt.Sprintf("%s · %d threads · %g GB · %g Gbit/s · accelerator %s", p.CPUName, p.Threads, p.RAMGB, p.NICGbps, accel))
	s.emitConfig("hardware")
	writeJSON(w, http.StatusOK, map[string]any{"profile": p})
}
