package ai

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Device is something object recognition can run on.
type Device struct {
	ID        string `json:"id"`   // "cpu" | "cuda" | "amd"
	Kind      string `json:"kind"` // "cpu" | "gpu"
	Name      string `json:"name"`
	Usable    bool   `json:"usable"`
	Reason    string `json:"reason,omitempty"`    // why it is not usable, in plain words
	Driver    string `json:"driver,omitempty"`    // e.g. the NVIDIA driver version
	Threads   int    `json:"threads,omitempty"`   // CPU threads Aperture may use
	PackID    string `json:"packId,omitempty"`    // runtime pack it needs
	Preferred bool   `json:"preferred,omitempty"` // what "Automatic" picks
}

// sysRoot lets tests point device discovery at a fake filesystem.
var sysRoot = "/"

func sysPath(p string) string { return filepath.Join(sysRoot, p) }

// Devices lists the CPU and any GPUs, best first. cpuThreads is how many
// threads Aperture's service may use (its CPU quota).
func Devices(cpuThreads int) []Device {
	cpu := Device{ID: "cpu", Kind: "cpu", Name: cpuName(), Usable: true, Threads: cpuThreads, PackID: "cpu"}
	var gpus []Device
	nvidia, amd := pciGPUs()
	if len(nvidia) > 0 || nvidiaDriverVersion() != "" {
		d := Device{ID: "cuda", Kind: "gpu", PackID: "cuda", Name: "NVIDIA GPU"}
		if names := nvidiaModels(); len(names) > 0 {
			d.Name = names[0]
		}
		d.Driver = nvidiaDriverVersion()
		switch {
		case d.Driver == "":
			d.Reason = "The NVIDIA driver isn't installed on this machine. Install it, restart, and Aperture will use the GPU."
		case !hasLibCUDA():
			d.Reason = "The NVIDIA driver is loaded but its CUDA library (libcuda.so.1) is missing. Reinstall the driver with its CUDA support."
		default:
			d.Usable = true
		}
		gpus = append(gpus, d)
	}
	if len(amd) > 0 {
		gpus = append(gpus, Device{
			ID: "amd", Kind: "gpu", Name: "AMD GPU",
			Reason: "AMD GPUs aren't supported for object recognition yet. Aperture runs it on the CPU instead.",
		})
	}
	out := append(gpus, cpu)
	for i := range out {
		if out[i].Usable {
			out[i].Preferred = true
			break
		}
	}
	return out
}

// PickDevice resolves a requested device ("auto", "cpu", "cuda") to a usable one.
func PickDevice(devs []Device, want string) Device {
	if want != "" && want != "auto" {
		for _, d := range devs {
			if d.ID == want && d.Usable {
				return d
			}
		}
	}
	for _, d := range devs {
		if d.Preferred {
			return d
		}
	}
	return devs[len(devs)-1]
}

// DefaultModel picks the model for a device: the best one that keeps up.
func DefaultModel(d Device) string {
	if d.Kind == "gpu" {
		return "yolox-m"
	}
	if d.Threads > 0 && d.Threads <= 2 {
		return "yolox-nano"
	}
	return "yolox-tiny"
}

func cpuName() string {
	f, err := os.Open(sysPath("proc/cpuinfo"))
	if err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if k, v, ok := strings.Cut(sc.Text(), ":"); ok && strings.TrimSpace(k) == "model name" {
				v = strings.TrimSpace(v)
				if v != "" {
					return v
				}
			}
		}
	}
	return runtime.GOARCH + " CPU"
}

// pciGPUs lists display controllers by vendor (NVIDIA 0x10de, AMD 0x1002).
func pciGPUs() (nvidia, amd []string) {
	dirs, _ := filepath.Glob(sysPath("sys/bus/pci/devices/*"))
	for _, d := range dirs {
		class := readTrim(filepath.Join(d, "class"))
		if !strings.HasPrefix(class, "0x03") { // display controller
			continue
		}
		switch readTrim(filepath.Join(d, "vendor")) {
		case "0x10de":
			nvidia = append(nvidia, filepath.Base(d))
		case "0x1002":
			amd = append(amd, filepath.Base(d))
		}
	}
	return
}

func nvidiaDriverVersion() string {
	s := readTrim(sysPath("proc/driver/nvidia/version"))
	// "NVRM version: NVIDIA UNIX x86_64 Kernel Module  575.64.03  Tue ..."
	// The open module says "… Open Kernel Module for x86_64  595.58.03 …":
	// take the first word shaped like a version.
	if i := strings.Index(s, "Kernel Module"); i >= 0 {
		for _, f := range strings.Fields(s[i+len("Kernel Module"):]) {
			if len(f) > 2 && f[0] >= '0' && f[0] <= '9' && strings.Contains(f, ".") {
				return f
			}
		}
	}
	if s != "" {
		return "loaded"
	}
	return ""
}

func nvidiaModels() []string {
	files, _ := filepath.Glob(sysPath("proc/driver/nvidia/gpus/*/information"))
	var out []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "Model" {
				out = append(out, strings.TrimSpace(v))
			}
		}
	}
	return out
}

// hasLibCUDA reports whether the driver's user-space CUDA library is installed.
func hasLibCUDA() bool {
	for _, p := range []string{
		"usr/lib/x86_64-linux-gnu/libcuda.so.1", "usr/lib64/libcuda.so.1", "usr/lib/libcuda.so.1",
		"usr/local/nvidia/lib64/libcuda.so.1", "usr/lib/wsl/lib/libcuda.so.1",
	} {
		if _, err := os.Stat(sysPath(p)); err == nil {
			return true
		}
	}
	return false
}

func readTrim(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
