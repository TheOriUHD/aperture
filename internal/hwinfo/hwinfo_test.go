package hwinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A Proxmox VM: generic CPU model, hypervisor flag, the service's cgroup limits.
func TestDetectVirtualMachineWithLimits(t *testing.T) {
	root := t.TempDir()
	write(t, root, "proc/cpuinfo", "processor\t: 0\nmodel name\t: QEMU Virtual CPU version 2.5+\nflags\t\t: fpu vme hypervisor lahf_lm\n")
	write(t, root, "proc/meminfo", "MemTotal:        6003648 kB\nMemFree: 1 kB\n")
	write(t, root, "proc/self/cgroup", "0::/system.slice/aperture.service\n")
	write(t, root, "sys/fs/cgroup/system.slice/aperture.service/cpu.max", "600000 100000\n")
	write(t, root, "sys/fs/cgroup/system.slice/aperture.service/memory.max", "3758096384\n")
	write(t, root, "sys/class/dmi/id/sys_vendor", "QEMU\n")
	write(t, root, "dev/dri/card0", "")

	in := Detect(root)
	if in.CPUModel != "QEMU Virtual CPU version 2.5+" {
		t.Errorf("cpu model %q", in.CPUModel)
	}
	if in.CPUQuota != 6 {
		t.Errorf("cpu quota %v, want 6", in.CPUQuota)
	}
	if in.MemTotalBytes != 6003648*1024 {
		t.Errorf("mem total %d", in.MemTotalBytes)
	}
	if in.MemLimitBytes != 3758096384 {
		t.Errorf("mem limit %d", in.MemLimitBytes)
	}
	if in.Virtual != "QEMU/KVM (Proxmox, libvirt)" {
		t.Errorf("virtual %q", in.Virtual)
	}
	if len(in.Accelerators) != 0 {
		t.Errorf("a virtual display adapter is not an accelerator: %+v", in.Accelerators)
	}
}

// A Raspberry Pi with a Coral USB stick and no cgroup limits.
func TestDetectBoardAndUSBCoral(t *testing.T) {
	root := t.TempDir()
	write(t, root, "proc/cpuinfo", "processor\t: 0\nBogoMIPS\t: 108.00\n\nHardware\t: BCM2835\nModel\t\t: Raspberry Pi 5 Model B Rev 1.0\n")
	write(t, root, "proc/device-tree/model", "Raspberry Pi 5 Model B Rev 1.0\x00")
	write(t, root, "proc/self/cgroup", "0::/\n")
	write(t, root, "sys/fs/cgroup/cpu.max", "max 100000\n")
	write(t, root, "sys/fs/cgroup/memory.max", "max\n")
	write(t, root, "sys/bus/usb/devices/2-1/idVendor", "18d1\n")
	write(t, root, "sys/bus/usb/devices/2-1/idProduct", "9302\n")

	in := Detect(root)
	if in.CPUModel != "Raspberry Pi 5 Model B Rev 1.0" || in.Board != "Raspberry Pi 5 Model B Rev 1.0" {
		t.Errorf("model %q board %q", in.CPUModel, in.Board)
	}
	if in.CPUQuota != 0 || in.MemLimitBytes != 0 {
		t.Errorf("unlimited cgroup read as quota %v / mem %d", in.CPUQuota, in.MemLimitBytes)
	}
	if in.Virtual != "" {
		t.Errorf("bare metal reported as %q", in.Virtual)
	}
	if len(in.Accelerators) != 1 || in.Accelerators[0].Name != "Google Coral USB Accelerator" {
		t.Errorf("accelerators %+v", in.Accelerators)
	}
}

// Bare-metal Intel box: Quick Sync render node and a PCIe Coral.
func TestDetectIntelGPUAndPCIeCoral(t *testing.T) {
	root := t.TempDir()
	write(t, root, "proc/cpuinfo", "model name\t: Intel(R) N100\nflags\t\t: fpu sse4_2 avx2\n")
	write(t, root, "dev/dri/renderD128", "")
	write(t, root, "sys/class/drm/renderD128/device/vendor", "0x8086\n")
	write(t, root, "dev/apex_0", "")

	in := Detect(root)
	if in.CPUModel != "Intel(R) N100" {
		t.Errorf("cpu model %q", in.CPUModel)
	}
	kinds := map[string]bool{}
	for _, a := range in.Accelerators {
		kinds[a.Name] = true
	}
	if !kinds["Intel GPU (Quick Sync)"] || !kinds["Google Coral (PCIe / M.2)"] {
		t.Errorf("accelerators %+v", in.Accelerators)
	}
}
