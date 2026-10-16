//go:build ignore

// SPDX-License-Identifier: AGPL-3.0-only
// Explicitly invoked by package-release.ps1; independent of host Unix file modes.
package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	directory := flag.String("directory", "", "Directory to archive")
	output := flag.String("output", "", "New .tar.gz path outside the input directory")
	epoch := flag.String("epoch", "", "RFC3339 commit timestamp")
	executable := flag.String("executable", "", "Relative path to mark executable")
	flag.Parse()
	if e := run(*directory, *output, *epoch, *executable); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run(directory, output, epoch, executable string) (err error) {
	if directory == "" || output == "" {
		return errors.New("directory and output are required")
	}
	root, e := filepath.Abs(directory)
	if e != nil {
		return e
	}
	out, e := filepath.Abs(output)
	if e != nil {
		return e
	}
	if out == root || strings.HasPrefix(out, root+string(filepath.Separator)) {
		return errors.New("output must be outside input directory")
	}
	date, e := time.Parse(time.RFC3339, epoch)
	if e != nil {
		return e
	}
	file, e := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if e != nil {
		return e
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	gz := gzip.NewWriter(file)
	defer func() { err = errors.Join(err, gz.Close()) }()
	tw := tar.NewWriter(gz)
	defer func() { err = errors.Join(err, tw.Close()) }()
	prefix := filepath.Base(root)
	return filepath.WalkDir(root, func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && entry.Name() == "node_modules" {
			return filepath.SkipDir
		}
		info, e := entry.Info()
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(root, filename)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, e = os.Readlink(filename)
			if e != nil {
				return e
			}
		}
		header, e := tar.FileInfoHeader(info, link)
		if e != nil {
			return e
		}
		header.Name = prefix
		if rel != "." {
			header.Name = path.Join(prefix, rel)
		}
		header.ModTime = date.UTC()
		header.AccessTime = time.Time{}
		header.ChangeTime = time.Time{}
		header.Uid = 0
		header.Gid = 0
		header.Uname = ""
		header.Gname = ""
		header.PAXRecords = nil
		if entry.IsDir() {
			header.Mode = 0755
			header.Name += "/"
		} else if info.Mode()&os.ModeSymlink != 0 {
			header.Mode = 0777
		} else if rel == executable {
			header.Mode = 0755
		} else {
			header.Mode = 0644
		}
		if e = tw.WriteHeader(header); e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		source, e := os.Open(filename)
		if e != nil {
			return e
		}
		_, copyErr := io.Copy(tw, source)
		return errors.Join(copyErr, source.Close())
	})
}
