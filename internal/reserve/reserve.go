// Package reserve hard-reserves disk space for Aperture with a preallocated
// "balloon" file. The balloon is sized so Aperture's total on-disk footprint
// (recorded footage + app data + balloon) always equals the operator's
// allocation. Because the balloon's blocks are really allocated (fallocate),
// the space is genuinely held — other programs can't use it, even while
// Aperture is stopped. As footage grows the balloon shrinks to keep the
// footprint constant; the retention janitor trims oldest footage at the ceiling.
package reserve

import (
	"os"
	"path/filepath"
	"syscall"
)

// FileName is the balloon file kept in the recordings directory.
const FileName = ".aperture-reservation"

// SafetyMargin is kept genuinely free on the filesystem so a reservation can
// never wedge the disk for the OS or other programs.
const SafetyMargin = int64(3) << 30 // 3 GiB

// Size returns the current balloon size in bytes (0 if absent).
func Size(dir string) int64 {
	fi, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		return 0
	}
	return fi.Size()
}

// Reconcile sizes the balloon in dir so Aperture's footprint equals allocation:
// balloon = allocation - usedBytes, clamped to what the filesystem can actually
// hold (always leaving SafetyMargin free). allocation <= 0 removes the balloon.
// Returns the resulting balloon size.
func Reconcile(dir string, allocation, usedBytes int64) (int64, error) {
	path := filepath.Join(dir, FileName)
	if allocation <= 0 {
		_ = os.Remove(path)
		return 0, nil
	}
	want := allocation - usedBytes
	if want < 0 {
		want = 0
	}
	cur := Size(dir)
	// Bavail already excludes the current balloon's blocks, so the most we can
	// grow to is (current size + free - margin).
	if _, avail, ok := statfs(dir); ok {
		if maxGrow := cur + avail - SafetyMargin; want > maxGrow {
			want = maxGrow
		}
	}
	if want < 0 {
		want = 0
	}
	if want == cur {
		return cur, nil
	}
	return resize(path, cur, want)
}

func resize(path string, cur, want int64) (int64, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return cur, err
	}
	defer f.Close()
	if want < cur {
		if err := f.Truncate(want); err != nil {
			return cur, err
		}
		return want, nil
	}
	// Grow: allocate real blocks over [0, want) so the space is truly consumed
	// (a plain truncate would leave a sparse file that reserves nothing).
	if err := syscall.Fallocate(int(f.Fd()), 0, 0, want); err != nil {
		return cur, err
	}
	return want, nil
}

func statfs(dir string) (total, avail int64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, 0, false
	}
	return int64(st.Blocks) * int64(st.Bsize), int64(st.Bavail) * int64(st.Bsize), true
}
