// Package hwinfo reports what the machine Aperture runs on can tell about itself:
// the CPU model, how many CPUs and how much memory Aperture may actually use (the
// service's cgroup limits, not just the host's), whether it is a virtual machine
// or container, and which video/AI accelerators are attached.
//
// It only reads files. Everything it reports is a starting point for the hardware
// profile an admin confirms in Settings: inside a VM the CPU model is often a
// generic "QEMU Virtual CPU", and only the admin knows the host it stands for.
package hwinfo

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Accelerator is a device an object detector or video pipeline could use.
type Accelerator struct {
	Kind string `json:"kind"` // tpu | npu | gpu | vpu
	Name string `json:"name"`
	Path string `json:"path"` // where it was found, for the curious
}

// Info is what detection found. Zero values mean "unknown" or "no limit".
type Info struct {
	CPUModel      string        `json:"cpuModel"`
	Board         string        `json:"board,omitempty"` // single-board computers: "Raspberry Pi 5 Model B Rev 1.0"
	Arch          string        `json:"arch"`
	LogicalCPUs   int           `json:"logicalCpus"`
	CPUQuota      float64       `json:"cpuQuota"` // cores Aperture's cgroup may use; 0 = unlimited
	MemTotalBytes uint64        `json:"memTotalBytes"`
	MemLimitBytes uint64        `json:"memLimitBytes"` // Aperture's cgroup memory.max; 0 = unlimited
	Virtual       string        `json:"virtual,omitempty"`
	Accelerators  []Accelerator `json:"accelerators"`
}

// Detect inspects the system. root is "/" in production; tests point it at a
// fake tree.
func Detect(root string) Info {
	in := Info{Arch: runtime.GOARCH, LogicalCPUs: runtime.NumCPU(), Accelerators: []Accelerator{}}
	cpuinfo := read(root, "proc/cpuinfo")
	in.CPUModel = cpuModel(cpuinfo)
	in.Board = strings.TrimSpace(strings.TrimRight(read(root, "proc/device-tree/model"), "\x00"))
	if in.Board == "" {
		in.Board = strings.TrimSpace(strings.TrimRight(read(root, "sys/firmware/devicetree/base/model"), "\x00"))
	}
	in.MemTotalBytes = memTotal(read(root, "proc/meminfo"))
	cg := ownCgroup(read(root, "proc/self/cgroup"))
	in.CPUQuota = cpuQuota(read(root, filepath.Join("sys/fs/cgroup", cg, "cpu.max")))
	in.MemLimitBytes = memLimit(read(root, filepath.Join("sys/fs/cgroup", cg, "memory.max")))
	in.Virtual = virtual(root, cpuinfo)
	in.Accelerators = accelerators(root)
	return in
}

func read(root, rel string) string {
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return ""
	}
	return string(b)
}

// cpuModel reads the x86 "model name", or what ARM kernels report instead.
func cpuModel(cpuinfo string) string {
	keys := []string{"model name", "Model", "Hardware", "cpu model", "Processor"}
	vals := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(cpuinfo))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if _, seen := vals[k]; !seen && v != "" {
			vals[k] = v
		}
	}
	for _, k := range keys {
		if v := vals[k]; v != "" {
			return v
		}
	}
	return ""
}

func memTotal(meminfo string) uint64 {
	sc := bufio.NewScanner(strings.NewReader(meminfo))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) >= 2 && f[0] == "MemTotal:" {
			kb, _ := strconv.ParseUint(f[1], 10, 64)
			return kb * 1024
		}
	}
	return 0
}

// ownCgroup is this process's cgroup v2 path ("0::/system.slice/aperture.service").
func ownCgroup(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if p, ok := strings.CutPrefix(line, "0::"); ok {
			return strings.TrimSpace(p)
		}
	}
	return ""
}

// cpuQuota turns cgroup v2 cpu.max ("600000 100000") into cores (6).
func cpuQuota(s string) float64 {
	f := strings.Fields(s)
	if len(f) != 2 || f[0] == "max" {
		return 0
	}
	q, err1 := strconv.ParseFloat(f[0], 64)
	p, err2 := strconv.ParseFloat(f[1], 64)
	if err1 != nil || err2 != nil || p <= 0 {
		return 0
	}
	return q / p
}

func memLimit(s string) uint64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "max" {
		return 0
	}
	v, _ := strconv.ParseUint(s, 10, 64)
	return v
}

// virtual names the hypervisor or container runtime, if any.
func virtual(root, cpuinfo string) string {
	if _, err := os.Stat(filepath.Join(root, ".dockerenv")); err == nil {
		return "Docker container"
	}
	if _, err := os.Stat(filepath.Join(root, "run/.containerenv")); err == nil {
		return "Podman container"
	}
	hyper := false
	for _, line := range strings.Split(cpuinfo, "\n") {
		if strings.HasPrefix(line, "flags") && strings.Contains(line, " hypervisor") {
			hyper = true
			break
		}
	}
	vendor := strings.TrimSpace(read(root, "sys/class/dmi/id/sys_vendor"))
	product := strings.TrimSpace(read(root, "sys/class/dmi/id/product_name"))
	known := map[string]string{
		"QEMU":                                  "QEMU/KVM (Proxmox, libvirt)",
		"VMware, Inc.":                          "VMware",
		"innotek GmbH":                          "VirtualBox",
		"Xen":                                   "Xen",
		"Parallels Software International Inc.": "Parallels",
		"Amazon EC2":                            "Amazon EC2",
		"Google":                                "Google Cloud",
	}
	if name, ok := known[vendor]; ok {
		return name
	}
	if vendor == "Microsoft Corporation" && strings.Contains(product, "Virtual") {
		return "Hyper-V"
	}
	if hyper {
		return "Virtual machine"
	}
	return ""
}

// accelerators finds AI and video accelerators by their device nodes and IDs.
func accelerators(root string) []Accelerator {
	out := []Accelerator{}
	glob := func(p string) []string {
		m, _ := filepath.Glob(filepath.Join(root, p))
		return m
	}
	rel := func(p string) string { r, _ := filepath.Rel(root, p); return "/" + r }

	for _, p := range glob("dev/apex_*") {
		out = append(out, Accelerator{Kind: "tpu", Name: "Google Coral (PCIe / M.2)", Path: rel(p)})
	}
	for _, p := range glob("dev/hailo*") {
		out = append(out, Accelerator{Kind: "npu", Name: "Hailo-8 / 8L", Path: rel(p)})
	}
	if ps := glob("dev/rknpu*"); len(ps) > 0 {
		out = append(out, Accelerator{Kind: "npu", Name: "Rockchip NPU", Path: rel(ps[0])})
	}
	for _, p := range glob("dev/accel/accel*") {
		drv, _ := os.Readlink(filepath.Join(root, "sys/class/accel", filepath.Base(p), "device/driver"))
		if strings.Contains(drv, "intel_vpu") {
			out = append(out, Accelerator{Kind: "npu", Name: "Intel NPU", Path: rel(p)})
		}
	}
	// USB sticks: Coral (before and after its firmware loads) and Intel's NCS2.
	for _, dev := range glob("sys/bus/usb/devices/*") {
		id := strings.TrimSpace(read(dev, "idVendor")) + ":" + strings.TrimSpace(read(dev, "idProduct"))
		switch id {
		case "1a6e:089a", "18d1:9302":
			out = append(out, Accelerator{Kind: "tpu", Name: "Google Coral USB Accelerator", Path: rel(dev)})
		case "03e7:2485":
			out = append(out, Accelerator{Kind: "vpu", Name: "Intel Neural Compute Stick 2", Path: rel(dev)})
		}
	}
	// GPUs with a render node. Virtual display adapters (virtio, QXL, VMware SVGA)
	// are skipped: they cannot decode or run a model.
	nvidia := false
	for _, p := range glob("dev/dri/renderD*") {
		vendor := strings.TrimSpace(read(root, filepath.Join("sys/class/drm", filepath.Base(p), "device/vendor")))
		switch vendor {
		case "0x8086":
			out = append(out, Accelerator{Kind: "gpu", Name: "Intel GPU (Quick Sync)", Path: rel(p)})
		case "0x1002":
			out = append(out, Accelerator{Kind: "gpu", Name: "AMD GPU", Path: rel(p)})
		case "0x10de":
			nvidia = true
		}
	}
	gpus := glob("proc/driver/nvidia/gpus/*/information")
	for _, info := range gpus {
		model := ""
		for _, line := range strings.Split(read(info, ""), "\n") {
			if v, ok := strings.CutPrefix(line, "Model:"); ok {
				model = strings.TrimSpace(v)
			}
		}
		if model == "" {
			model = "NVIDIA GPU"
		} else if !strings.HasPrefix(model, "NVIDIA") {
			model = "NVIDIA " + model
		}
		out = append(out, Accelerator{Kind: "gpu", Name: model, Path: rel(filepath.Dir(info))})
	}
	if len(gpus) == 0 && (nvidia || len(glob("dev/nvidia0")) > 0) {
		out = append(out, Accelerator{Kind: "gpu", Name: "NVIDIA GPU", Path: "/dev/nvidia0"})
	}
	return out
}
