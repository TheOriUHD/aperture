package storage

import (
	"context"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	smb2 "github.com/cloudsoda/go-smb2"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const probeFile = ".aperture-connect-test"

// Probe runs a destination's connection checklist without saving it, returning
// per-step results for the setup wizard. It never mutates real footage — it only
// creates (and removes) a tiny probe file.
func Probe(spec *Spec) ProbeResult {
	switch spec.Type {
	case TypeSMB:
		return probeSMB(spec)
	case TypeSFTP:
		return probeSFTP(spec)
	case TypeLocal, "":
		return probeLocal(spec)
	default:
		return ProbeResult{OK: false, Steps: []Step{{ID: "type", Label: "Unknown destination type", Status: StepFail}}}
	}
}

// stepper accumulates checklist steps and short-circuits on the first failure.
type stepper struct {
	steps []Step
	ok    bool
}

func (s *stepper) pass(id, label, detail string) { s.steps = append(s.steps, Step{id, label, StepPass, detail}) }
func (s *stepper) warn(id, label, detail string) { s.steps = append(s.steps, Step{id, label, StepWarn, detail}) }
func (s *stepper) fail(id, label, detail string) {
	s.steps = append(s.steps, Step{id, label, StepFail, detail})
	s.ok = false
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return strconv.FormatInt(b, 10) + " B"
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return strconv.FormatFloat(float64(b)/float64(div), 'f', 1, 64) + " " + string("KMGTPE"[exp]) + "B"
}

/* ---------------- local ---------------- */

func probeLocal(spec *Spec) ProbeResult {
	s := &stepper{ok: true}
	dir := strings.TrimSpace(cfgStr(spec.Config, "path"))
	if dir == "" {
		s.fail("path", "Check the folder path", "A folder path is required.")
		return ProbeResult{OK: s.ok, Steps: s.steps}
	}
	existed := true
	if _, err := os.Stat(dir); err != nil {
		existed = false
		if err := os.MkdirAll(dir, 0o755); err != nil {
			s.fail("reach", "Create the folder", "Could not create it: "+err.Error())
			return ProbeResult{OK: s.ok, Steps: s.steps}
		}
	}
	if existed {
		s.pass("reach", "Reach the folder", "Found the existing folder.")
	} else {
		s.pass("reach", "Create the folder", "Created a new folder here.")
	}
	probe := filepath.Join(dir, probeFile)
	if err := os.WriteFile(probe, []byte("aperture-ok"), 0o644); err != nil {
		s.fail("write", "Write a test file", "Folder is not writable: "+err.Error())
		return ProbeResult{OK: s.ok, Steps: s.steps}
	}
	got, err := os.ReadFile(probe)
	_ = os.Remove(probe)
	if err != nil || string(got) != "aperture-ok" {
		s.fail("read", "Read it back", "Could not verify the probe file.")
		return ProbeResult{OK: s.ok, Steps: s.steps}
	}
	s.pass("write", "Write a test file", "Wrote a probe file successfully.")
	s.pass("read", "Read it back", "Verified read-back matches.")
	res := ProbeResult{OK: s.ok, Steps: s.steps}
	if b, err := newLocal(dir).Stat(); err == nil && b.Total > 0 {
		s.pass("capacity", "Read capacity", humanBytes(b.Free)+" free of "+humanBytes(b.Total))
		res.DiskTotalBytes, res.DiskFreeBytes = b.Total, b.Free
	} else {
		s.warn("capacity", "Read capacity", "Capacity could not be determined.")
	}
	res.Steps = s.steps
	return res
}

/* ---------------- smb ---------------- */

func probeSMB(spec *Spec) ProbeResult {
	s := &stepper{ok: true}
	b := smbConfig(spec)
	if b.host == "" || b.share == "" {
		s.fail("config", "Check the details", "Host and share are required.")
		return ProbeResult{OK: s.ok, Steps: s.steps}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	d := &smb2.Dialer{Initiator: &smb2.NTLMInitiator{User: b.user, Password: b.password, Domain: b.domain}}
	sess, err := d.Dial(ctx, net.JoinHostPort(b.host, strconv.Itoa(b.port)))
	if err != nil {
		s.fail("connect", "Connect & sign in", friendly(err))
		return ProbeResult{OK: s.ok, Steps: s.steps}
	}
	defer sess.Logoff()
	s.pass("connect", "Connect & sign in", "Signed in to "+b.host+".")

	mount, err := sess.Mount(b.share)
	if err != nil {
		s.fail("mount", "Open the share", friendly(err))
		return ProbeResult{OK: s.ok, Steps: s.steps}
	}
	defer mount.Umount()
	s.pass("mount", "Open the share", "Opened “"+b.share+"”.")

	dir := b.subfolder
	if dir != "" {
		if err := mount.MkdirAll(dir, 0o755); err != nil {
			s.fail("subfolder", "Create the subfolder", friendly(err))
			return ProbeResult{OK: s.ok, Steps: s.steps}
		}
		s.pass("subfolder", "Create the subfolder", "Ready at “"+dir+"”.")
	}

	probe := path.Join(dir, probeFile)
	f, err := mount.Create(probe)
	if err != nil {
		s.fail("write", "Write a test file", friendly(err))
		return ProbeResult{OK: s.ok, Steps: s.steps}
	}
	_, wErr := f.Write([]byte("aperture-ok"))
	_ = f.Close()
	if wErr != nil {
		_ = mount.Remove(probe)
		s.fail("write", "Write a test file", friendly(wErr))
		return ProbeResult{OK: s.ok, Steps: s.steps}
	}
	s.pass("write", "Write a test file", "Wrote a probe file.")

	got, rErr := mount.ReadFile(probe)
	_ = mount.Remove(probe)
	if rErr != nil || string(got) != "aperture-ok" {
		s.fail("read", "Read it back", "Could not verify the probe file.")
		return ProbeResult{OK: s.ok, Steps: s.steps}
	}
	s.pass("read", "Read it back", "Verified read-back matches.")

	res := ProbeResult{OK: s.ok}
	name := dir
	if name == "" {
		name = "."
	}
	if info, err := mount.Statfs(name); err == nil {
		bs := int64(info.BlockSize())
		total, free := int64(info.TotalBlockCount())*bs, int64(info.AvailableBlockCount())*bs
		s.pass("capacity", "Read capacity", humanBytes(free)+" free of "+humanBytes(total))
		res.DiskTotalBytes, res.DiskFreeBytes = total, free
	} else {
		s.warn("capacity", "Read capacity", "Capacity could not be determined.")
	}
	res.Steps = s.steps
	return res
}

/* ---------------- sftp ---------------- */

func probeSFTP(spec *Spec) ProbeResult {
	s := &stepper{ok: true}
	b := sftpConfig(spec)
	if b.host == "" || b.user == "" {
		s.fail("config", "Check the details", "Host and user are required.")
		return ProbeResult{OK: s.ok, Steps: s.steps}
	}

	var observed string
	cfg, err := b.clientConfig(&observed)
	if err != nil {
		s.fail("auth", "Prepare credentials", friendly(err))
		return ProbeResult{OK: s.ok, Steps: s.steps}
	}
	conn, err := ssh.Dial("tcp", net.JoinHostPort(b.host, strconv.Itoa(b.port)), cfg)
	if err != nil {
		s.fail("connect", "Connect & sign in", friendly(err))
		return ProbeResult{OK: s.ok, Steps: s.steps}
	}
	defer conn.Close()
	detail := "Signed in to " + b.host + "."
	if b.hostKey == "" && observed != "" {
		detail += " Host key " + observed
	}
	s.pass("connect", "Connect & sign in", detail)

	cli, err := sftp.NewClient(conn)
	if err != nil {
		s.fail("sftp", "Start SFTP", friendly(err))
		return ProbeResult{OK: s.ok, Steps: s.steps}
	}
	defer cli.Close()
	s.pass("sftp", "Start SFTP", "SFTP subsystem ready.")

	base := b.base
	if base != "" {
		if err := cli.MkdirAll(base); err != nil {
			s.fail("folder", "Create the folder", friendly(err))
			return ProbeResult{OK: s.ok, Steps: s.steps}
		}
		s.pass("folder", "Create the folder", "Ready at “"+base+"”.")
	}

	probe := path.Join(base, probeFile)
	f, err := cli.Create(probe)
	if err != nil {
		s.fail("write", "Write a test file", friendly(err))
		return ProbeResult{OK: s.ok, Steps: s.steps}
	}
	_, wErr := f.Write([]byte("aperture-ok"))
	_ = f.Close()
	if wErr != nil {
		_ = cli.Remove(probe)
		s.fail("write", "Write a test file", friendly(wErr))
		return ProbeResult{OK: s.ok, Steps: s.steps}
	}
	s.pass("write", "Write a test file", "Wrote a probe file.")

	rf, err := cli.Open(probe)
	var got []byte
	if err == nil {
		got = make([]byte, 16)
		n, _ := rf.Read(got)
		got = got[:n]
		_ = rf.Close()
	}
	_ = cli.Remove(probe)
	if err != nil || string(got) != "aperture-ok" {
		s.fail("read", "Read it back", "Could not verify the probe file.")
		return ProbeResult{OK: s.ok, Steps: s.steps}
	}
	s.pass("read", "Read it back", "Verified read-back matches.")

	res := ProbeResult{OK: s.ok, HostKey: observed}
	statBase := base
	if statBase == "" {
		statBase = "."
	}
	if vfs, err := cli.StatVFS(statBase); err == nil {
		total, free := int64(vfs.TotalSpace()), int64(vfs.FreeSpace())
		s.pass("capacity", "Read capacity", humanBytes(free)+" free of "+humanBytes(total))
		res.DiskTotalBytes, res.DiskFreeBytes = total, free
	} else {
		s.warn("capacity", "Read capacity", "Server doesn’t report capacity (soft cap only).")
	}
	res.Steps = s.steps
	return res
}

// friendly trims noisy transport prefixes from an error for display.
func friendly(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 && i < len(msg)-2 && len(msg)-i < 60 {
		return msg
	}
	return fmt.Sprintf("%v", err)
}
