package ai

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// GPUSetup is a step-by-step check of what this machine needs before Aperture
// can use an NVIDIA GPU. Aperture never installs a driver itself; it looks at
// the system and says exactly what to run, for this distribution, kernel and
// card. The commands follow NVIDIA's own driver installation guide
// (docs.nvidia.com/datacenter/tesla/driver-installation-guide).
type GPUSetup struct {
	GPU        *GPUInfo    `json:"gpu,omitempty"`
	OS         OSInfo      `json:"os"`
	Kernel     string      `json:"kernel"`
	VM         string      `json:"vm,omitempty"`         // hypervisor, when this is a virtual machine
	SecureBoot string      `json:"secureBoot,omitempty"` // on | off | "" (not an EFI system / unknown)
	Driver     string      `json:"driver,omitempty"`     // loaded driver version
	DriverOpen bool        `json:"driverOpen,omitempty"` // the loaded kernel module is the open one
	Ready      bool        `json:"ready"`                // the driver works; Aperture can use the GPU
	SMI        string      `json:"smi,omitempty"`        // nvidia-smi's one-line answer, when it ran
	Sudo       bool        `json:"sudo"`                 // commands are written for a user with sudo
	Steps      []SetupStep `json:"steps"`
}

// GPUInfo is the NVIDIA card on the PCI bus.
type GPUInfo struct {
	PCI      string `json:"pci"`
	DeviceID string `json:"deviceId"`
	Name     string `json:"name"`
	Family   string `json:"family"` // Blackwell, Ada Lovelace, … ("" when unknown)
	// OpenModules: NVIDIA's open kernel modules support the card (Turing and
	// newer). OpenRequired: only they do (Blackwell and newer).
	OpenModules  bool   `json:"openModules"`
	OpenRequired bool   `json:"openRequired"`
	MinDriver    int    `json:"minDriver,omitempty"` // lowest driver branch that knows the card
	Branch       string `json:"branch"`              // the driver branch to install
}

// OSInfo is /etc/os-release, plus NVIDIA's name for its package repository.
type OSInfo struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	Codename  string `json:"codename,omitempty"`
	Pretty    string `json:"pretty"`
	RepoDist  string `json:"repoDist,omitempty"` // e.g. debian13; "" when NVIDIA has no repository for it
	Supported bool   `json:"supported"`
}

// SetupStep is one step of the guide.
type SetupStep struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	State    string   `json:"state"` // done | todo | warn | later
	Detail   string   `json:"detail,omitempty"`
	Commands []string `json:"commands,omitempty"`
	Note     string   `json:"note,omitempty"`
	Link     string   `json:"link,omitempty"`
}

// CheckGPUSetup looks at the machine. It runs nothing that changes it; at most
// nvidia-smi, to confirm a driver answers.
func CheckGPUSetup() GPUSetup {
	s := GPUSetup{OS: readOS(), Kernel: kernelRelease(), VM: hypervisor(), SecureBoot: secureBoot()}
	// Most people run commands as themselves with sudo; a system without sudo
	// is administered as root, so its commands go without.
	if _, err := os.Stat(sysPath("usr/bin/sudo")); err == nil {
		s.Sudo = true
	}
	s.GPU = findNVIDIA()
	s.Driver = nvidiaDriverVersion()
	if v := readTrim(sysPath("proc/driver/nvidia/version")); strings.Contains(v, "Open Kernel Module") {
		s.DriverOpen = true
	}
	if s.Driver != "" {
		if out, err := runQuiet("nvidia-smi", "--query-gpu=name,driver_version,memory.total", "--format=csv,noheader"); err == nil {
			s.SMI = strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
		}
	}
	s.Ready = s.Driver != "" && hasLibCUDA()
	s.Steps = s.steps()
	return s
}

const nvGuide = "https://docs.nvidia.com/datacenter/tesla/driver-installation-guide/"

// su prefixes a command that needs root. apt commands that would stop to ask
// "Do you want to continue?" get -y: the guide already said what they do.
func (s GPUSetup) su(cmd string) string {
	for _, verb := range []string{"apt -V install ", "apt install ", "apt purge ", "apt autoremove"} {
		if strings.HasPrefix(cmd, verb) {
			cmd = "apt -y " + strings.TrimPrefix(cmd, "apt ")
			break
		}
	}
	if s.Sudo {
		return "sudo " + cmd
	}
	return cmd
}

// toFile writes text to a root-owned file.
func (s GPUSetup) toFile(text, path string) string {
	if s.Sudo {
		return "printf '" + text + "' | sudo tee " + path + " > /dev/null"
	}
	return "printf '" + text + "' > " + path
}

func (s GPUSetup) steps() []SetupStep {
	var out []SetupStep
	add := func(st SetupStep) { out = append(out, st) }
	gpu := s.GPU

	// 1. The card is visible.
	visible := SetupStep{ID: "visible", Title: "This machine sees the GPU"}
	switch {
	case gpu != nil:
		visible.State = "done"
		visible.Detail = fmt.Sprintf("%s on PCI %s.", gpu.Name, gpu.PCI)
	case s.VM != "":
		visible.State = "todo"
		visible.Detail = "No NVIDIA card is visible. This is a virtual machine (" + s.VM + "), so the host must hand the card to it (PCI passthrough)."
		visible.Note = "On Proxmox: shut this VM down, then in the VM's Hardware tab choose Add › PCI Device, pick the GPU, tick All Functions (and PCI-Express if the machine type is q35), and start the VM again. The host needs IOMMU enabled and the card free of host drivers."
		visible.Link = "https://pve.proxmox.com/wiki/PCI_Passthrough"
	default:
		visible.State = "todo"
		visible.Detail = "No NVIDIA card is visible on this machine's PCI bus. Check that it's seated and powered."
	}
	add(visible)
	if gpu == nil {
		add(SetupStep{ID: "driver", Title: "Install the NVIDIA driver", State: "later", Detail: "Once the card is visible, this guide shows the exact commands for it."})
		return out
	}

	// Unsupported distribution: point at NVIDIA's guide.
	if !s.OS.Supported {
		st := SetupStep{ID: "driver", Title: "Install the NVIDIA driver", Link: nvGuide}
		if s.Driver != "" {
			st.State = "done"
			st.Detail = "Driver " + s.Driver + " is loaded."
		} else {
			st.State = "todo"
			st.Detail = fmt.Sprintf("Aperture has step-by-step commands for Debian 12/13 and Ubuntu 22.04/24.04/26.04. For %s, follow NVIDIA's guide for your distribution and install the %s driver.", s.OS.Pretty, flavour(gpu))
		}
		out = append(out, st)
		out = append(out, s.finalSteps()...)
		return out
	}

	// 2. Kernel headers: the driver's kernel module is compiled for the kernel.
	add(s.headersStep())

	// 3. NVIDIA's package repository (the distribution's own driver may be too
	// old for the card, and mixing both breaks upgrades).
	repo := SetupStep{ID: "repo", Title: "Add NVIDIA's package repository"}
	if hasCUDARepo() || s.Driver != "" {
		repo.State = "done"
		repo.Detail = "NVIDIA's repository is set up."
	} else {
		repo.State = "todo"
		repo.Detail = "It carries the current driver for every supported card. " + s.OS.Pretty + "'s own NVIDIA packages are older and don't know the newest cards."
		var cmds []string
		if s.OS.ID == "debian" {
			if c := contribCommand(); c != "" {
				cmds = append(cmds, s.su(c))
			}
		}
		cmds = append(cmds,
			"wget https://developer.download.nvidia.com/compute/cuda/repos/"+s.OS.RepoDist+"/x86_64/cuda-keyring_1.1-1_all.deb",
			s.su("dpkg -i cuda-keyring_1.1-1_all.deb"),
			s.su("apt update"),
		)
		repo.Commands = cmds
		if len(cmds) > 4 {
			repo.Note = "The first command adds Debian's contrib section to the package sources (NVIDIA's packages need it); running it twice changes nothing."
		}
		if hasDistroNVIDIA() {
			repo.Note = "NVIDIA packages from " + s.OS.Pretty + " itself are installed. Remove them first so the two don't mix: " + s.su("apt purge '*nvidia*'") + " && " + s.su("apt autoremove")
		}
	}
	add(repo)

	// 4. The driver itself, compute only (no desktop parts on a server).
	drv := SetupStep{ID: "driver", Title: "Install the NVIDIA driver (" + flavour(gpu) + ")", Link: nvGuide + "kernel-modules.html"}
	switch {
	case s.Driver != "" && gpu.MinDriver > 0 && majorOf(s.Driver) < gpu.MinDriver:
		drv.State = "warn"
		drv.Detail = fmt.Sprintf("Driver %s is loaded, but %s needs %d or newer.", s.Driver, gpu.Name, gpu.MinDriver)
		drv.Commands = []string{s.su("apt update"), s.su("apt -V install " + driverPackages(gpu, s.OS))}
	case s.Driver != "" && gpu.OpenRequired && !s.DriverOpen:
		drv.State = "warn"
		drv.Detail = gpu.Name + " only works with NVIDIA's open kernel modules, and the loaded driver uses the proprietary ones."
		drv.Commands = []string{s.su("apt -V install " + driverPackages(gpu, s.OS))}
	case s.Driver != "":
		drv.State = "done"
		kind := "proprietary"
		if s.DriverOpen {
			kind = "open"
		}
		drv.Detail = "Driver " + s.Driver + " is loaded (" + kind + " kernel modules)."
	case nvidiaInstalledNotLoaded():
		drv.State = "done"
		drv.Detail = "The driver is installed; it starts after a restart."
	case !gpu.OpenModules:
		// NVIDIA's guide now only covers the open modules; older cards need the
		// proprietary ones of the 580 branch. Point there rather than guess.
		drv.State = "warn"
		drv.Detail = driverWhy(gpu)
		drv.Commands = []string{s.su("apt install nvidia-driver-pinning-580")}
		drv.Note = "Then install the 580 driver with the proprietary kernel modules as NVIDIA's guide describes for " + s.OS.Pretty + "."
		drv.Link = nvGuide + "version-locking.html"
	default:
		drv.State = "todo"
		drv.Detail = driverWhy(gpu)
		drv.Commands = []string{s.su("apt -V install " + driverPackages(gpu, s.OS))}
		if s.SecureBoot == "on" {
			drv.Note = "Secure Boot is on: the driver's kernel module must be signed with a key you enrol. During the install you'll be asked for a one-time password; at the next start a blue MOK Manager screen appears (on a VM, in its console). Choose Enroll MOK › Continue › Yes, enter that password, and reboot."
		}
	}
	add(drv)

	out = append(out, s.finalSteps()...)
	return out
}

// finalSteps: nouveau out of the way, restart, and the check.
func (s GPUSetup) finalSteps() []SetupStep {
	var out []SetupStep
	if nouveauLoaded() && nouveauBlacklisted() {
		out = append(out, SetupStep{
			ID: "nouveau", Title: "Turn off the nouveau driver", State: "done",
			Detail: "Blacklisted; it stays off from the next start.",
		})
	} else if nouveauLoaded() {
		out = append(out, SetupStep{
			ID: "nouveau", Title: "Turn off the nouveau driver", State: "todo",
			Detail: "Linux's built-in nouveau driver has the card and keeps NVIDIA's driver from loading.",
			Commands: []string{
				s.toFile("blacklist nouveau\\noptions nouveau modeset=0\\n", "/etc/modprobe.d/blacklist-nouveau.conf"),
				// Every installed kernel, including one installed for the headers.
				s.su("update-initramfs -u -k all"),
			},
		})
	}
	restart := SetupStep{ID: "restart", Title: "Restart this machine"}
	if s.Driver != "" {
		restart.State = "done"
		restart.Detail = "The driver is running."
	} else {
		restart.State = "todo"
		restart.Detail = "The driver loads at the next start. Aperture comes back by itself and recording resumes."
		if k := newestKernel(); k != "" && k != s.Kernel {
			restart.Detail = "This starts the new kernel (" + k + ") with the NVIDIA driver. Aperture comes back by itself and recording resumes."
		}
		restart.Commands = []string{s.su("reboot")}
	}
	out = append(out, restart)
	check := SetupStep{ID: "check", Title: "Check the GPU answers"}
	switch {
	case s.Ready:
		check.State = "done"
		check.Detail = "Driver " + s.Driver + " and its CUDA library are in place."
		if s.SMI != "" {
			check.Detail = "nvidia-smi: " + s.SMI
		}
	case s.Driver != "" && !hasLibCUDA():
		check.State = "warn"
		check.Detail = "The driver is loaded but its CUDA library (libcuda.so.1) is missing. Install the compute part of the driver."
		if s.OS.ID == "ubuntu" {
			check.Commands = []string{s.su("apt -V install libnvidia-compute")}
		} else {
			check.Commands = []string{s.su("apt -V install nvidia-driver-cuda")}
		}
	default:
		check.State = "later"
		check.Detail = "After the restart, nvidia-smi lists the card. Aperture then offers to switch object recognition to it."
		check.Commands = []string{"nvidia-smi"}
	}
	out = append(out, check)
	return out
}

func flavour(g *GPUInfo) string {
	if g.OpenModules {
		return "open kernel modules"
	}
	return "580 branch, proprietary kernel modules"
}

func driverWhy(g *GPUInfo) string {
	switch {
	case g.OpenRequired:
		return g.Name + " needs NVIDIA's open kernel modules (the proprietary ones don't support it) and driver " + strconv.Itoa(g.MinDriver) + " or newer. This installs the current driver, compute part only."
	case g.OpenModules:
		return "The open kernel modules are NVIDIA's recommended choice for " + g.Name + ". This installs the current driver, compute part only (no desktop components)."
	default:
		return g.Name + " is older than the RTX 20 series: current drivers no longer support it, so this stays on the 580 branch, which still does, with the proprietary kernel modules."
	}
}

// driverPackages is what NVIDIA's guide installs for a headless, compute-only
// system with the open kernel modules.
func driverPackages(_ *GPUInfo, o OSInfo) string {
	if o.ID == "ubuntu" {
		return "libnvidia-compute nvidia-dkms-open"
	}
	return "nvidia-driver-cuda nvidia-kernel-open-dkms"
}

// ---- looking at the machine ----------------------------------------------------

func findNVIDIA() *GPUInfo {
	dirs, _ := filepath.Glob(sysPath("sys/bus/pci/devices/*"))
	for _, d := range dirs {
		if !strings.HasPrefix(readTrim(filepath.Join(d, "class")), "0x03") || readTrim(filepath.Join(d, "vendor")) != "0x10de" {
			continue
		}
		dev := strings.TrimPrefix(readTrim(filepath.Join(d, "device")), "0x")
		g := &GPUInfo{PCI: filepath.Base(d), DeviceID: dev}
		id, _ := strconv.ParseUint(dev, 16, 32)
		g.Family, g.OpenModules, g.OpenRequired, g.MinDriver = familyOf(uint32(id))
		g.Branch = "latest"
		if !g.OpenModules {
			g.Branch = "580"
		}
		g.Name = pciName("10de", dev)
		if names := nvidiaModels(); len(names) > 0 {
			g.Name = names[0]
		}
		if g.Name == "" {
			g.Name = "NVIDIA GPU (device " + dev + ")"
		}
		return g
	}
	return nil
}

// familyOf maps an NVIDIA PCI device id to its architecture. Device ids grow
// with each generation; the boundaries are the first id of each.
func familyOf(id uint32) (family string, open, openRequired bool, minDriver int) {
	switch {
	case id >= 0x2900: // GB100/GB20x: RTX 50, B100/B200
		return "Blackwell", true, true, 570
	case id >= 0x2600 && id < 0x2900: // AD10x: RTX 40
		return "Ada Lovelace", true, false, 0
	case id >= 0x2300 && id < 0x2340: // GH100
		return "Hopper", true, false, 0
	case id >= 0x2200 && id < 0x2600: // GA10x: RTX 30, A-series
		return "Ampere", true, false, 0
	case id >= 0x1e00 && id < 0x2200: // TU10x/TU11x: RTX 20, GTX 16
		return "Turing", true, false, 0
	case id >= 0x1d80 && id < 0x1e00: // GV100
		return "Volta", false, false, 0
	case id >= 0x15f0 && id < 0x1d80: // GP10x: GTX 10
		return "Pascal", false, false, 0
	default:
		return "", false, false, 0
	}
}

// pciName looks a device up in the PCI id database, if installed.
func pciName(vendor, device string) string {
	for _, p := range []string{"usr/share/misc/pci.ids", "usr/share/hwdata/pci.ids"} {
		f, err := os.Open(sysPath(p))
		if err != nil {
			continue
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		inVendor := false
		for sc.Scan() {
			line := sc.Text()
			if line == "" || line[0] == '#' {
				continue
			}
			if line[0] != '\t' {
				inVendor = strings.HasPrefix(line, vendor+" ")
				continue
			}
			if inVendor && len(line) > 1 && line[1] != '\t' && strings.HasPrefix(line[1:], device+" ") {
				name := strings.TrimSpace(line[1+len(device):])
				// "GB206 [GeForce RTX 5060]" → "NVIDIA GeForce RTX 5060"
				if i, j := strings.Index(name, "["), strings.LastIndex(name, "]"); i >= 0 && j > i {
					name = name[i+1 : j]
				}
				return "NVIDIA " + name
			}
		}
		return ""
	}
	return ""
}

func readOS() OSInfo {
	o := OSInfo{Pretty: "Linux"}
	b, err := os.ReadFile(sysPath("etc/os-release"))
	if err != nil {
		return o
	}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"`)
		switch k {
		case "ID":
			o.ID = v
		case "VERSION_ID":
			o.Version = v
		case "VERSION_CODENAME":
			o.Codename = v
		case "PRETTY_NAME":
			o.Pretty = v
		}
	}
	switch {
	case o.ID == "debian" && (o.Version == "12" || o.Version == "13"):
		o.RepoDist = "debian" + o.Version
	case o.ID == "ubuntu" && (o.Version == "22.04" || o.Version == "24.04" || o.Version == "26.04"):
		o.RepoDist = "ubuntu" + strings.ReplaceAll(o.Version, ".", "")
	}
	o.Supported = o.RepoDist != ""
	return o
}

func kernelRelease() string {
	if v := readTrim(sysPath("proc/sys/kernel/osrelease")); v != "" {
		return v
	}
	return "unknown"
}

// headersStep: the NVIDIA kernel module is compiled for each kernel, so the
// kernel's headers must be installed. Debian only keeps the headers of its
// newest kernel in the archive (a machine that hasn't been updated for a while
// can't get the ones for the kernel it runs), so on Debian this installs the
// kernel and header meta-packages: the newest kernel with matching headers,
// which then follow every kernel update so the driver keeps being rebuilt.
func (s GPUSetup) headersStep() SetupStep {
	st := SetupStep{ID: "headers", Title: "Kernel headers"}
	flav := debianFlavour(s.Kernel)
	if s.OS.ID == "debian" && flav != "" {
		st.Title = "Current kernel with its headers"
	}
	switch {
	case s.Driver != "":
		st.State = "done"
		st.Detail = "The driver is running."
	case s.OS.ID == "debian" && flav != "" && dpkgInstalled("linux-headers-"+flav):
		st.State = "done"
		st.Detail = "Installed; they follow the kernel from now on."
		if k := newestKernel(); k != "" && k != s.Kernel {
			st.Detail += " Kernel " + k + " is installed and starts with the restart below."
		}
	case hasKernelHeaders(s.Kernel):
		st.State = "done"
		st.Detail = "Present for kernel " + s.Kernel + "."
	case s.OS.ID == "debian" && flav != "":
		st.State = "todo"
		st.Detail = "The driver's kernel module is compiled for the kernel and needs its headers. Debian only keeps headers for its newest kernel, so this installs the newest kernel together with its headers. They then stay in step with every kernel update, so the driver keeps working. The restart at the end starts the new kernel."
		st.Commands = []string{s.su("apt update"), s.su("apt install linux-image-" + flav + " linux-headers-" + flav)}
	default:
		st.State = "todo"
		st.Detail = "The driver's kernel module is compiled for the running kernel (" + s.Kernel + ") and needs its headers."
		st.Commands = []string{s.su("apt update"), s.su("apt install linux-headers-$(uname -r)")}
	}
	return st
}

// debianFlavour is the kernel flavour of a Debian kernel release
// ("6.12.101+deb13-amd64" → "amd64", "…+deb13-cloud-amd64" → "cloud-amd64");
// "" for a kernel that isn't Debian's own.
func debianFlavour(kernel string) string {
	i := strings.Index(kernel, "+deb")
	if i < 0 {
		return ""
	}
	_, f, ok := strings.Cut(kernel[i:], "-")
	if !ok || f == "" {
		return ""
	}
	return f
}

// dpkgInstalled reports whether a package is installed.
func dpkgInstalled(pkg string) bool {
	b, err := os.ReadFile(sysPath("var/lib/dpkg/status"))
	if err != nil {
		return false
	}
	for _, block := range strings.Split(string(b), "\n\n") {
		if strings.HasPrefix(block, "Package: "+pkg+"\n") && strings.Contains(block, "\nStatus: install ok installed") {
			return true
		}
	}
	return false
}

// newestKernel is the newest kernel in /boot (the one the next start boots).
func newestKernel() string {
	m, _ := filepath.Glob(sysPath("boot/vmlinuz-*"))
	best := ""
	for _, p := range m {
		k := strings.TrimPrefix(filepath.Base(p), "vmlinuz-")
		if best == "" || kernelLess(best, k) {
			best = k
		}
	}
	return best
}

// kernelLess compares kernel releases by their numbers ("6.12.94" < "6.12.113").
func kernelLess(a, b string) bool {
	na, nb := kernelNums(a), kernelNums(b)
	for i := 0; i < len(na) && i < len(nb); i++ {
		if na[i] != nb[i] {
			return na[i] < nb[i]
		}
	}
	return len(na) < len(nb)
}

func kernelNums(k string) []int {
	var out []int
	n, in := 0, false
	for _, c := range k {
		if c >= '0' && c <= '9' {
			n, in = n*10+int(c-'0'), true
			continue
		}
		if in {
			out = append(out, n)
			n, in = 0, false
		}
		if c != '.' && c != '-' {
			break // "+deb13…": the rest isn't the version
		}
	}
	if in {
		out = append(out, n)
	}
	return out
}

func hasKernelHeaders(kernel string) bool {
	_, err := os.Stat(sysPath(filepath.Join("lib/modules", kernel, "build")))
	return err == nil
}

func hasCUDARepo() bool {
	if _, err := os.Stat(sysPath("usr/share/keyrings/cuda-archive-keyring.gpg")); err == nil {
		return true
	}
	m, _ := filepath.Glob(sysPath("etc/apt/sources.list.d/cuda*"))
	return len(m) > 0
}

// hasDistroNVIDIA: the distribution's own NVIDIA driver packages are installed.
func hasDistroNVIDIA() bool {
	b, err := os.ReadFile(sysPath("var/lib/dpkg/status"))
	if err != nil {
		return false
	}
	return strings.Contains(string(b), "\nPackage: nvidia-kernel-dkms\nStatus: install ok installed") ||
		strings.Contains(string(b), "\nPackage: nvidia-driver\nStatus: install ok installed")
}

// nvidiaInstalledNotLoaded: the driver's packages are there, but not running yet.
func nvidiaInstalledNotLoaded() bool {
	if nvidiaDriverVersion() != "" {
		return false
	}
	_, err := os.Stat(sysPath("usr/bin/nvidia-smi"))
	return err == nil
}

// nouveauBlacklisted: a modprobe.d file keeps nouveau from loading.
func nouveauBlacklisted() bool {
	m, _ := filepath.Glob(sysPath("etc/modprobe.d/*.conf"))
	for _, p := range m {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.TrimSpace(line) == "blacklist nouveau" {
				return true
			}
		}
	}
	return false
}

func nouveauLoaded() bool {
	_, err := os.Stat(sysPath("sys/module/nouveau"))
	return err == nil
}

// contribCommand adds Debian's contrib component where it's missing (NVIDIA's
// guide asks for it). It targets whichever sources file the system uses.
func contribCommand() string {
	if b, err := os.ReadFile(sysPath("etc/apt/sources.list.d/debian.sources")); err == nil {
		if !strings.Contains(string(b), "contrib") {
			return `sed -i -E '/^Components:/{/\bcontrib\b/!s/\bmain\b/main contrib/}' /etc/apt/sources.list.d/debian.sources`
		}
		return ""
	}
	if b, err := os.ReadFile(sysPath("etc/apt/sources.list")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			l := strings.TrimSpace(line)
			if strings.HasPrefix(l, "deb ") && !strings.Contains(l, " contrib") {
				return `sed -i -E '/^deb(-src)? /{/\bcontrib\b/!s/\bmain\b/main contrib/}' /etc/apt/sources.list`
			}
		}
	}
	return ""
}

func hypervisor() string {
	if v := readTrim(sysPath("sys/class/dmi/id/sys_vendor")); v != "" {
		switch {
		case strings.Contains(v, "QEMU"):
			return "QEMU/KVM, e.g. Proxmox"
		case strings.Contains(v, "VMware"):
			return "VMware"
		case strings.Contains(v, "Microsoft"):
			return "Hyper-V"
		case strings.Contains(v, "Xen"):
			return "Xen"
		}
	}
	if strings.Contains(readTrim(sysPath("proc/cpuinfo")), "hypervisor") {
		return "a hypervisor"
	}
	return ""
}

// secureBoot reads the EFI SecureBoot variable (4 attribute bytes, then the value).
func secureBoot() string {
	m, _ := filepath.Glob(sysPath("sys/firmware/efi/efivars/SecureBoot-*"))
	if len(m) == 0 {
		return ""
	}
	b, err := os.ReadFile(m[0])
	if err != nil || len(b) < 5 {
		return ""
	}
	if b[4] == 1 {
		return "on"
	}
	return "off"
}

func majorOf(v string) int {
	n, _ := strconv.Atoi(strings.SplitN(v, ".", 2)[0])
	return n
}

func runQuiet(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}
