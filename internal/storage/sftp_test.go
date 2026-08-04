package storage

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// startTestSFTP spins up an in-process SFTP server (pkg/sftp over an x/crypto/ssh
// loopback) serving the real filesystem, so the SFTP backend can be tested end to
// end with no external server or privileges.
func startTestSFTP(t *testing.T) (host string, port int) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == "user" && string(pass) == "pass" {
				return nil, nil
			}
			return nil, os.ErrPermission
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSFTP(conn, cfg)
		}
	}()

	h, p, _ := net.SplitHostPort(ln.Addr().String())
	pn, _ := strconv.Atoi(p)
	return h, pn
}

func serveSFTP(nConn net.Conn, cfg *ssh.ServerConfig) {
	sConn, chans, reqs, err := ssh.NewServerConn(nConn, cfg)
	if err != nil {
		return
	}
	defer sConn.Close()
	go ssh.DiscardRequests(reqs)
	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(ssh.UnknownChannelType, "only sessions")
			continue
		}
		ch, requests, err := newCh.Accept()
		if err != nil {
			continue
		}
		go func(in <-chan *ssh.Request) {
			for req := range in {
				ok := req.Type == "subsystem" && len(req.Payload) >= 4 && string(req.Payload[4:]) == "sftp"
				_ = req.Reply(ok, nil)
			}
		}(requests)
		srv, err := sftp.NewServer(ch)
		if err != nil {
			continue
		}
		// When the client closes its side, Serve returns; close the channel so the
		// client's reader sees EOF and its Close() completes (no hang).
		go func(c ssh.Channel) {
			_ = srv.Serve()
			_ = srv.Close()
			_ = c.Close()
		}(ch)
	}
}

func sftpSpec(t *testing.T, host string, port int) *Spec {
	return &Spec{
		Type:   TypeSFTP,
		Config: map[string]any{"host": host, "port": float64(port), "user": "user", "path": t.TempDir()},
		Secret: "pass",
	}
}

func TestSFTPProbe(t *testing.T) {
	host, port := startTestSFTP(t)
	res := Probe(sftpSpec(t, host, port))
	if !res.OK {
		t.Fatalf("probe failed: %+v", res.Steps)
	}
	// Connect, SFTP, folder, write, read must all pass.
	byID := map[string]string{}
	for _, s := range res.Steps {
		byID[s.id()] = s.Status
	}
	for _, id := range []string{"connect", "sftp", "write", "read"} {
		if byID[id] != StepPass {
			t.Fatalf("step %q = %q, want pass (steps=%+v)", id, byID[id], res.Steps)
		}
	}
}

func TestSFTPBackendRoundTrip(t *testing.T) {
	host, port := startTestSFTP(t)
	b, err := newSFTP(sftpSpec(t, host, port))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	// A local source file to upload.
	src := filepath.Join(t.TempDir(), "seg.ts")
	payload := []byte("hello-sftp-segment")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	rel := "cam1/1700000000.ts"
	n, err := b.Put(src, rel)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("put wrote %d bytes, want %d", n, len(payload))
	}

	rc, size, err := b.Open(rel)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if size != int64(len(payload)) {
		t.Fatalf("open size %d, want %d", size, len(payload))
	}
	buf := make([]byte, size)
	if _, err := rc.Read(buf); err != nil && err.Error() != "EOF" {
		// short reads are fine; verify below
	}
	_ = rc.Close()
	if string(buf) != string(payload) {
		t.Fatalf("read back %q, want %q", buf, payload)
	}

	if err := b.Remove(rel); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, _, err := b.Open(rel); err == nil {
		t.Fatal("expected open to fail after remove")
	}
}

// id extracts a step's id (unexported field access within the package test).
func (s Step) id() string { return s.ID }
