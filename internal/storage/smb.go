package storage

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	smb2 "github.com/cloudsoda/go-smb2"
)

// smbBackend records to an SMB/CIFS share. It keeps one session + mounted share,
// lazily (re)connecting, and serves footage back over the same connection.
type smbBackend struct {
	host      string
	port      int
	share     string
	subfolder string
	user      string
	domain    string
	password  string

	mu    sync.Mutex
	sess  *smb2.Session
	mount *smb2.Share
}

func smbConfig(spec *Spec) *smbBackend {
	return &smbBackend{
		host:      strings.TrimSpace(cfgStr(spec.Config, "host")),
		port:      cfgInt(spec.Config, "port", 445),
		share:     strings.TrimSpace(cfgStr(spec.Config, "share")),
		subfolder: strings.Trim(strings.TrimSpace(cfgStr(spec.Config, "subfolder")), "/"),
		user:      cfgStr(spec.Config, "user"),
		domain:    cfgStr(spec.Config, "domain"),
		password:  spec.Secret,
	}
}

func newSMB(spec *Spec) (Backend, error) {
	b := smbConfig(spec)
	if b.host == "" || b.share == "" {
		return nil, fmt.Errorf("smb: host and share are required")
	}
	return b, nil
}

func (b *smbBackend) full(rel string) string {
	return path.Join(b.subfolder, strings.TrimPrefix(rel, "/"))
}

// share returns a connected, mounted share, dialing on first use / after a drop.
func (b *smbBackend) getShare() (*smb2.Share, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.mount != nil {
		return b.mount, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	d := &smb2.Dialer{Initiator: &smb2.NTLMInitiator{User: b.user, Password: b.password, Domain: b.domain}}
	sess, err := d.Dial(ctx, net.JoinHostPort(b.host, strconv.Itoa(b.port)))
	if err != nil {
		return nil, fmt.Errorf("smb dial: %w", err)
	}
	mount, err := sess.Mount(b.share)
	if err != nil {
		_ = sess.Logoff()
		return nil, fmt.Errorf("smb mount %q: %w", b.share, err)
	}
	b.sess, b.mount = sess, mount
	return mount, nil
}

// drop tears down the cached connection so the next call reconnects.
func (b *smbBackend) drop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.mount != nil {
		_ = b.mount.Umount()
		b.mount = nil
	}
	if b.sess != nil {
		_ = b.sess.Logoff()
		b.sess = nil
	}
}

func (b *smbBackend) EnsureDir(rel string) error {
	s, err := b.getShare()
	if err != nil {
		return err
	}
	if err := s.MkdirAll(path.Dir(b.full(rel)), 0o755); err != nil {
		b.drop()
		return err
	}
	return nil
}

func (b *smbBackend) Put(localPath, rel string) (int64, error) {
	s, err := b.getShare()
	if err != nil {
		return 0, err
	}
	dst := b.full(rel)
	if err := s.MkdirAll(path.Dir(dst), 0o755); err != nil {
		b.drop()
		return 0, err
	}
	in, err := os.Open(localPath)
	if err != nil {
		return 0, err
	}
	defer in.Close()

	tmp := dst + ".part"
	_ = s.Remove(tmp)
	out, err := s.Create(tmp)
	if err != nil {
		b.drop()
		return 0, err
	}
	n, cErr := io.Copy(out, in)
	closeErr := out.Close()
	if cErr != nil || closeErr != nil {
		_ = s.Remove(tmp)
		b.drop()
		if cErr != nil {
			return 0, cErr
		}
		return 0, closeErr
	}
	_ = s.Remove(dst) // Rename won't overwrite on some servers
	if err := s.Rename(tmp, dst); err != nil {
		_ = s.Remove(tmp)
		b.drop()
		return 0, err
	}
	return n, nil
}

func (b *smbBackend) Open(rel string) (ReadSeekCloser, int64, error) {
	s, err := b.getShare()
	if err != nil {
		return nil, 0, err
	}
	f, err := s.Open(b.full(rel))
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

func (b *smbBackend) Remove(rel string) error {
	s, err := b.getShare()
	if err != nil {
		return err
	}
	if err := s.Remove(b.full(rel)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (b *smbBackend) Stat() (Stat, error) {
	s, err := b.getShare()
	if err != nil {
		return Stat{}, err
	}
	name := b.subfolder
	if name == "" {
		name = "."
	}
	info, err := s.Statfs(name)
	if err != nil {
		return Stat{}, err
	}
	bs := int64(info.BlockSize())
	return Stat{
		Total:     int64(info.TotalBlockCount()) * bs,
		Free:      int64(info.AvailableBlockCount()) * bs,
		Supported: true,
	}, nil
}

func (b *smbBackend) Close() error {
	b.drop()
	return nil
}
