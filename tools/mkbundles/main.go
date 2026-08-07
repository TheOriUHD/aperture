// Command mkbundles builds every extension example under examples/ into a
// distributable .zip in examples/dist/. It exists so a shipped bundle can never
// drift from the example source it represents (a stale bundle fails on install —
// a bad first experience; TestExampleBundlesAreFresh fails the build if one does).
//
// The output is DETERMINISTIC — files stored (not compressed) in sorted order with a
// fixed timestamp — so rebuilding an unchanged example produces byte-identical bytes
// and the committed zip does not churn. Run via `make examples`.
package main

import (
	"archive/zip"
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// fixedTime keeps zip entries reproducible; the real mtime is irrelevant to install.
var fixedTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

func main() {
	src := flag.String("src", "examples", "directory of example bundles")
	out := flag.String("out", filepath.Join("examples", "dist"), "output directory for .zip bundles")
	flag.Parse()

	names, err := exampleDirs(*src)
	if err != nil {
		fatal(err)
	}
	if len(names) == 0 {
		fmt.Println("no examples found under", *src)
		return
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal(err)
	}
	for _, name := range names {
		zpath := filepath.Join(*out, name+".zip")
		data, err := buildZip(filepath.Join(*src, name))
		if err != nil {
			fatal(fmt.Errorf("%s: %w", name, err))
		}
		changed, err := writeIfChanged(zpath, data)
		if err != nil {
			fatal(err)
		}
		state := "up to date"
		if changed {
			state = "built"
		}
		fmt.Printf("%-24s %s (%d bytes)\n", name+".zip", state, len(data))
	}
}

// exampleDirs returns the names of immediate subdirectories of src that contain a
// manifest.json (so the dist/ output dir and any stray files are skipped).
func exampleDirs(src string) ([]string, error) {
	entries, err := os.ReadDir(src)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(src, e.Name(), "manifest.json")); err == nil {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// buildZip zips every file under dir into a deterministic archive: sorted, stored
// (uncompressed, so the bytes don't depend on the compressor), fixed timestamps.
func buildZip(dir string) ([]byte, error) {
	var rels []string
	if err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rels = append(rels, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(rels)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, rel := range rels {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		hdr := &zip.FileHeader{Name: rel, Method: zip.Store, Modified: fixedTime}
		hdr.SetMode(0o644)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeIfChanged writes data only when it differs from what is already on disk, so an
// unchanged bundle keeps its file mtime and produces no git churn.
func writeIfChanged(path string, data []byte) (bool, error) {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return false, nil
	}
	return true, os.WriteFile(path, data, 0o644)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "mkbundles:", err)
	os.Exit(1)
}
