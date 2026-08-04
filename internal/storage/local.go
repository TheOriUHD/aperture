package storage

import (
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// localBackend stores segments on a directory of the local filesystem. It is the
// backend for the default location and any local storage location. Absolute rel
// paths (legacy segments) are used as-is; relative ones join the root.
type localBackend struct {
	root string
}

func newLocal(root string) *localBackend { return &localBackend{root: root} }

func (b *localBackend) resolve(rel string) string {
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(b.root, filepath.FromSlash(rel))
}

func (b *localBackend) EnsureDir(rel string) error {
	return os.MkdirAll(filepath.Dir(b.resolve(rel)), 0o755)
}

func (b *localBackend) Put(localPath, rel string) (int64, error) {
	dst := b.resolve(rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	tmp := dst + ".part"
	n, err := copyFile(localPath, tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	return n, nil
}

func (b *localBackend) Open(rel string) (ReadSeekCloser, int64, error) {
	f, err := os.Open(b.resolve(rel))
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

func (b *localBackend) Remove(rel string) error {
	if err := os.Remove(b.resolve(rel)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (b *localBackend) Stat() (Stat, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(b.root, &st); err != nil {
		return Stat{}, err
	}
	total := int64(st.Blocks) * int64(st.Bsize)
	free := int64(st.Bavail) * int64(st.Bsize)
	return Stat{Total: total, Free: free, Supported: true}, nil
}

func (b *localBackend) Close() error { return nil }

// copyFile copies src to dst (created/truncated) and returns bytes written.
func copyFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return n, err
}
