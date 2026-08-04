package storage

import (
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// sftpBackend records to a directory on a remote server over SSH/SFTP.
type sftpBackend struct {
	host     string
	port     int
	user     string
	base     string
	authMode string // "password" | "key"
	secret   string // password or PEM private key
	hostKey  string // pinned SHA256 fingerprint ("" = trust on first use)

	mu  sync.Mutex
	ssh *ssh.Client
	cli *sftp.Client
}

func sftpConfig(spec *Spec) *sftpBackend {
	return &sftpBackend{
		host:     strings.TrimSpace(cfgStr(spec.Config, "host")),
		port:     cfgInt(spec.Config, "port", 22),
		user:     strings.TrimSpace(cfgStr(spec.Config, "user")),
		base:     strings.TrimRight(strings.TrimSpace(cfgStr(spec.Config, "path")), "/"),
		authMode: cfgStr(spec.Config, "authMode"),
		secret:   spec.Secret,
		hostKey:  cfgStr(spec.Config, "hostKey"),
	}
}

func newSFTP(spec *Spec) (Backend, error) {
	b := sftpConfig(spec)
	if b.host == "" || b.user == "" {
		return nil, fmt.Errorf("sftp: host and user are required")
	}
	return b, nil
}

// hostKeyCallback pins to a known fingerprint if one is set, otherwise trusts the
// server on first use (recording the fingerprint into *observed for the caller).
func hostKeyCallback(pinned string, observed *string) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		fp := ssh.FingerprintSHA256(key)
		if observed != nil {
			*observed = fp
		}
		if pinned != "" && fp != pinned {
			return fmt.Errorf("host key mismatch (server offered %s)", fp)
		}
		return nil
	}
}

func (b *sftpBackend) clientConfig(observed *string) (*ssh.ClientConfig, error) {
	var auth ssh.AuthMethod
	if b.authMode == "key" {
		signer, err := ssh.ParsePrivateKey([]byte(b.secret))
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		auth = ssh.PublicKeys(signer)
	} else {
		auth = ssh.Password(b.secret)
	}
	return &ssh.ClientConfig{
		User:            b.user,
		Auth:            []ssh.AuthMethod{auth},
		HostKeyCallback: hostKeyCallback(b.hostKey, observed),
		Timeout:         20 * time.Second,
	}, nil
}

func (b *sftpBackend) full(rel string) string {
	return path.Join(b.base, strings.TrimPrefix(rel, "/"))
}

func (b *sftpBackend) getClient() (*sftp.Client, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cli != nil {
		return b.cli, nil
	}
	cfg, err := b.clientConfig(nil)
	if err != nil {
		return nil, err
	}
	conn, err := ssh.Dial("tcp", net.JoinHostPort(b.host, strconv.Itoa(b.port)), cfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial: %w", err)
	}
	cli, err := sftp.NewClient(conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("sftp: %w", err)
	}
	b.ssh, b.cli = conn, cli
	return cli, nil
}

func (b *sftpBackend) drop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cli != nil {
		_ = b.cli.Close()
		b.cli = nil
	}
	if b.ssh != nil {
		_ = b.ssh.Close()
		b.ssh = nil
	}
}

func (b *sftpBackend) EnsureDir(rel string) error {
	c, err := b.getClient()
	if err != nil {
		return err
	}
	if err := c.MkdirAll(path.Dir(b.full(rel))); err != nil {
		b.drop()
		return err
	}
	return nil
}

func (b *sftpBackend) Put(localPath, rel string) (int64, error) {
	c, err := b.getClient()
	if err != nil {
		return 0, err
	}
	dst := b.full(rel)
	if err := c.MkdirAll(path.Dir(dst)); err != nil {
		b.drop()
		return 0, err
	}
	in, err := os.Open(localPath)
	if err != nil {
		return 0, err
	}
	defer in.Close()

	tmp := dst + ".part"
	_ = c.Remove(tmp)
	out, err := c.Create(tmp)
	if err != nil {
		b.drop()
		return 0, err
	}
	n, cErr := io.Copy(out, in)
	closeErr := out.Close()
	if cErr != nil || closeErr != nil {
		_ = c.Remove(tmp)
		b.drop()
		if cErr != nil {
			return 0, cErr
		}
		return 0, closeErr
	}
	_ = c.Remove(dst)
	if err := c.Rename(tmp, dst); err != nil {
		_ = c.Remove(tmp)
		b.drop()
		return 0, err
	}
	return n, nil
}

func (b *sftpBackend) Open(rel string) (ReadSeekCloser, int64, error) {
	c, err := b.getClient()
	if err != nil {
		return nil, 0, err
	}
	f, err := c.Open(b.full(rel))
	if err != nil {
		return nil, 0, err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, err
	}
	return f, fi.Size(), nil
}

func (b *sftpBackend) Remove(rel string) error {
	c, err := b.getClient()
	if err != nil {
		return err
	}
	if err := c.Remove(b.full(rel)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (b *sftpBackend) Stat() (Stat, error) {
	c, err := b.getClient()
	if err != nil {
		return Stat{}, err
	}
	base := b.base
	if base == "" {
		base = "."
	}
	vfs, err := c.StatVFS(base)
	if err != nil {
		// Server lacks the statvfs@openssh.com extension — capacity unknown.
		return Stat{Supported: false}, nil
	}
	return Stat{Total: int64(vfs.TotalSpace()), Free: int64(vfs.FreeSpace()), Supported: true}, nil
}

func (b *sftpBackend) Close() error {
	b.drop()
	return nil
}
