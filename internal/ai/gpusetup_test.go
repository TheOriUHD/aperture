package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeRoot(t *testing.T) func(p, s string) {
	t.Helper()
	root := t.TempDir()
	old := sysRoot
	sysRoot = root
	t.Cleanup(func() { sysRoot = old })
	return func(p, s string) {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755)
		_ = os.WriteFile(filepath.Join(root, p), []byte(s), 0o644)
	}
}

func step(s GPUSetup, id string) SetupStep {
	for _, st := range s.Steps {
		if st.ID == id {
			return st
		}
	}
	return SetupStep{}
}

func TestGPUSetupGuidesADebianVMToItsRTX5060(t *testing.T) {
	write := fakeRoot(t)
	write("etc/os-release", "PRETTY_NAME=\"Debian GNU/Linux 13 (trixie)\"\nID=debian\nVERSION_ID=\"13\"\nVERSION_CODENAME=trixie\n")
	write("proc/sys/kernel/osrelease", "6.12.101+deb13-amd64\n")
	write("sys/class/dmi/id/sys_vendor", "QEMU\n")
	write("sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c", "\x06\x00\x00\x00\x00")
	write("etc/apt/sources.list", "deb http://deb.debian.org/debian/ trixie main non-free-firmware\n")
	write("usr/share/misc/pci.ids", "10de  NVIDIA Corporation\n\t2d04  GB206 [GeForce RTX 5060 Ti]\n\t2d05  GB206 [GeForce RTX 5060]\n1af4  Red Hat, Inc.\n")
	write("usr/bin/sudo", "")
	write("boot/vmlinuz-6.12.94+deb13-amd64", "")
	write("boot/vmlinuz-6.12.101+deb13-amd64", "")

	// Not passed through yet: the guide says how, and nothing else yet.
	s := CheckGPUSetup()
	if s.GPU != nil || step(s, "visible").State != "todo" || !strings.Contains(step(s, "visible").Note, "Proxmox") || s.VM == "" {
		t.Fatalf("before passthrough: %+v", s)
	}

	// Passed through, nothing installed.
	write("sys/bus/pci/devices/0000:01:00.0/class", "0x030000\n")
	write("sys/bus/pci/devices/0000:01:00.0/vendor", "0x10de\n")
	write("sys/bus/pci/devices/0000:01:00.0/device", "0x2d05\n")
	s = CheckGPUSetup()
	if s.GPU == nil || s.GPU.Name != "NVIDIA GeForce RTX 5060" || s.GPU.Family != "Blackwell" || !s.GPU.OpenRequired || s.GPU.MinDriver != 570 {
		t.Fatalf("gpu = %+v", s.GPU)
	}
	if s.SecureBoot != "off" || !s.OS.Supported || s.OS.RepoDist != "debian13" {
		t.Fatalf("system = %+v / %s", s.OS, s.SecureBoot)
	}
	// The running kernel is older than Debian's archive keeps headers for: install
	// the meta-packages (newest kernel + headers) instead.
	if st := step(s, "headers"); st.State != "todo" || len(st.Commands) != 2 ||
		st.Commands[1] != "sudo apt -y install linux-image-amd64 linux-headers-amd64" {
		t.Fatalf("headers = %+v", st)
	}
	repo := step(s, "repo")
	if repo.State != "todo" || !strings.Contains(strings.Join(repo.Commands, "\n"), "repos/debian13/x86_64/cuda-keyring_1.1-1_all.deb") ||
		!strings.HasPrefix(repo.Commands[0], "sudo sed -i") || !strings.Contains(repo.Commands[0], "contrib") ||
		strings.HasPrefix(repo.Commands[1], "sudo") /* wget runs as you */ || repo.Commands[2] != "sudo dpkg -i cuda-keyring_1.1-1_all.deb" {
		t.Fatalf("repo = %+v", repo)
	}
	drv := step(s, "driver")
	if drv.State != "todo" || drv.Commands[len(drv.Commands)-1] != "sudo apt -y -V install nvidia-driver-cuda nvidia-kernel-open-dkms" || drv.Note != "" {
		t.Fatalf("driver = %+v", drv)
	}
	if s.Ready || step(s, "restart").State != "todo" || step(s, "restart").Commands[0] != "sudo reboot" {
		t.Fatal("not ready yet")
	}

	// Kernel + headers installed: the step ticks off and the restart names the
	// new kernel; nouveau (loaded) is blacklisted for every kernel.
	write("var/lib/dpkg/status", "Package: linux-image-amd64\nStatus: install ok installed\nVersion: 6.12.113-1\n\nPackage: linux-headers-amd64\nStatus: install ok installed\nVersion: 6.12.113-1\n")
	write("boot/vmlinuz-6.12.113+deb13-amd64", "")
	write("sys/module/nouveau/refcnt", "1\n")
	s = CheckGPUSetup()
	if st := step(s, "headers"); st.State != "done" || !strings.Contains(st.Detail, "6.12.113+deb13-amd64") {
		t.Fatalf("headers after install = %+v", st)
	}
	if st := step(s, "restart"); !strings.Contains(st.Detail, "6.12.113+deb13-amd64") {
		t.Fatalf("restart = %+v", st)
	}
	nv := step(s, "nouveau")
	if nv.State != "todo" || !strings.Contains(nv.Commands[0], "| sudo tee /etc/modprobe.d/blacklist-nouveau.conf") || nv.Commands[1] != "sudo update-initramfs -u -k all" {
		t.Fatalf("nouveau = %+v", nv)
	}

	// Installed and restarted: an old closed driver would be flagged...
	write("proc/driver/nvidia/version", "NVRM version: NVIDIA UNIX x86_64 Kernel Module  550.163.01  Release Build\n")
	s = CheckGPUSetup()
	if st := step(s, "driver"); st.State != "warn" || !strings.Contains(st.Detail, "570") {
		t.Fatalf("old driver not flagged: %+v", st)
	}
	// ...the right one is ready.
	write("proc/driver/nvidia/version", "NVRM version: NVIDIA UNIX Open Kernel Module for x86_64  595.58.03  Release Build\n")
	write("usr/lib/x86_64-linux-gnu/libcuda.so.1", "")
	s = CheckGPUSetup()
	if !s.Ready || !s.DriverOpen || s.Driver != "595.58.03" || step(s, "driver").State != "done" || step(s, "check").State != "done" {
		t.Fatalf("ready: %+v", s)
	}
}

func TestGPUSetupOnUbuntuWithSecureBoot(t *testing.T) {
	write := fakeRoot(t)
	write("etc/os-release", "PRETTY_NAME=\"Ubuntu 24.04.3 LTS\"\nID=ubuntu\nVERSION_ID=\"24.04\"\n")
	write("proc/sys/kernel/osrelease", "6.8.0-60-generic\n")
	write("sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c", "\x06\x00\x00\x00\x01")
	write("sys/bus/pci/devices/0000:01:00.0/class", "0x030000\n")
	write("sys/bus/pci/devices/0000:01:00.0/vendor", "0x10de\n")
	write("sys/bus/pci/devices/0000:01:00.0/device", "0x2684\n") // RTX 4090
	s := CheckGPUSetup()
	if s.GPU.Family != "Ada Lovelace" || s.GPU.OpenRequired || !s.GPU.OpenModules || s.OS.RepoDist != "ubuntu2404" {
		t.Fatalf("setup = %+v / %+v", s.GPU, s.OS)
	}
	drv := step(s, "driver")
	if drv.Commands[len(drv.Commands)-1] != "apt -y -V install libnvidia-compute nvidia-dkms-open" || !strings.Contains(drv.Note, "MOK") {
		t.Fatalf("driver = %+v", drv)
	}
	if strings.Contains(strings.Join(step(s, "repo").Commands, " "), "contrib") {
		t.Fatal("Ubuntu doesn't need Debian's contrib")
	}
}

func TestGPUSetupOlderCard(t *testing.T) {
	write := fakeRoot(t)
	write("etc/os-release", "ID=debian\nVERSION_ID=\"12\"\nPRETTY_NAME=\"Debian 12\"\n")
	write("sys/bus/pci/devices/0000:01:00.0/class", "0x030000\n")
	write("sys/bus/pci/devices/0000:01:00.0/vendor", "0x10de\n")
	write("sys/bus/pci/devices/0000:01:00.0/device", "0x1b80\n") // GTX 1080
	s := CheckGPUSetup()
	if s.GPU.Family != "Pascal" || s.GPU.OpenModules || s.GPU.Branch != "580" {
		t.Fatalf("gpu = %+v", s.GPU)
	}
	if st := step(s, "driver"); st.State != "warn" || st.Commands[0] != "apt -y install nvidia-driver-pinning-580" || st.Link == "" {
		t.Fatalf("driver = %+v", st)
	}
}

func TestKernelHelpers(t *testing.T) {
	if f := debianFlavour("6.12.101+deb13-amd64"); f != "amd64" {
		t.Fatal(f)
	}
	if f := debianFlavour("6.12.101+deb13-cloud-amd64"); f != "cloud-amd64" {
		t.Fatal(f)
	}
	if f := debianFlavour("6.8.12-4-pve"); f != "" {
		t.Fatal(f)
	}
	if !kernelLess("6.12.94+deb13-amd64", "6.12.113+deb13-amd64") || kernelLess("6.12.113+deb13-amd64", "6.12.101+deb13-amd64") {
		t.Fatal("kernel ordering")
	}
}
