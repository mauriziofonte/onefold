// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package tooldl

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/bodgit/sevenzip"

	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/statedir"
)

// Phases reported by Progress.
const (
	PhaseQueued   = "queued"
	PhaseDownload = "download"
	PhaseExtract  = "extract"
	PhaseDone     = "done"
	PhaseError    = "error"
)

// Cancelled is the Progress.Err of an install stopped by its context.
const Cancelled = "cancelled"

// Progress is the state of one package install.
type Progress struct {
	ID    string `json:"id"`
	Phase string `json:"phase"`
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
	Err   string `json:"err,omitempty"`
}

// Installer downloads, verifies and unpacks packages into Dir.
type Installer struct {
	Dir        string       // managed tools dir (tools.ManagedDir)
	Client     *http.Client // nil = a client without overall timeout (big files)
	OnProgress func(Progress)
}

// Installed is what installed.json records per package id.
type Installed struct {
	Version string    `json:"version"`
	SHA256  string    `json:"sha256"`
	Files   []string  `json:"files"`
	At      time.Time `json:"at"`
}

const manifestName = "installed.json"

// ReadInstalled returns the packages recorded in dir (empty when none).
func ReadInstalled(dir string) map[string]Installed {
	m := map[string]Installed{}
	b, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

func (in *Installer) report(p Progress) {
	if in.OnProgress != nil {
		in.OnProgress(p)
	}
}

// Install downloads p (resuming a partial download left by an interrupted
// run), checks size and SHA-256, unpacks the mapped files and records them.
// Files of the same package from an older install are replaced.
func (in *Installer) Install(ctx context.Context, p Package) error {
	err := in.install(ctx, p)
	if err != nil {
		msg := err.Error()
		if errors.Is(err, context.Canceled) {
			msg = Cancelled // the partial download stays: the next attempt resumes it
		}
		in.report(Progress{ID: p.ID, Phase: PhaseError, Total: p.Size, Err: msg})
		return err
	}
	in.report(Progress{ID: p.ID, Phase: PhaseDone, Done: p.Size, Total: p.Size})
	return nil
}

func (in *Installer) install(ctx context.Context, p Package) error {
	dl := filepath.Join(in.Dir, ".download")
	if err := os.MkdirAll(dl, statedir.DirMode); err != nil {
		return err
	}
	part := filepath.Join(dl, p.ID+"-"+p.Version+fsx.TempSuffix) // kept across runs: resumed
	if err := in.download(ctx, p, part); err != nil {
		return err
	}
	in.report(Progress{ID: p.ID, Phase: PhaseExtract, Done: p.Size, Total: p.Size})
	staging := filepath.Join(in.Dir, ".staging-"+p.ID)
	os.RemoveAll(staging) // leftover of an interrupted extract: ours, never user data
	defer os.RemoveAll(staging)
	files, err := extract(p, part, staging)
	if err != nil {
		return fmt.Errorf("%s: unpack: %w", p.Name, err)
	}
	old := ReadInstalled(in.Dir)[p.ID]
	for _, f := range old.Files {
		if rel := filepath.FromSlash(strings.TrimSuffix(f, "/")); filepath.IsLocal(rel) {
			os.RemoveAll(filepath.Join(in.Dir, rel))
		}
	}
	for _, f := range topLevel(files) {
		dst := filepath.Join(in.Dir, f)
		os.RemoveAll(dst)
		if err := os.Rename(filepath.Join(staging, f), dst); err != nil {
			return fmt.Errorf("%s: install %s: %w (is the tool running?)", p.Name, f, err)
		}
	}
	if err := in.record(p, topLevel(files)); err != nil {
		return err
	}
	os.Remove(part)
	return nil
}

// download fills part up to p.Size, resuming with a Range request, and
// verifies the SHA-256 of the whole file. A mismatch deletes part.
func (in *Installer) download(ctx context.Context, p Package, part string) error {
	h := sha256.New()
	var have int64
	if f, err := os.Open(part); err == nil {
		have, _ = io.Copy(h, f)
		f.Close()
	}
	if have > p.Size {
		os.Remove(part)
		h.Reset()
		have = 0
	}
	if have < p.Size {
		var err error
		if have, err = in.fetch(ctx, p, part, have, h); err != nil {
			return err
		}
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if have != p.Size || sum != p.SHA256 {
		os.Remove(part)
		return fmt.Errorf("%s: downloaded file does not match the pinned checksum (size %d, sha256 %s): discarded", p.Name, have, sum)
	}
	return nil
}

func (in *Installer) fetch(ctx context.Context, p Package, part string, have int64, h hash.Hash) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return have, err
	}
	req.Header.Set("User-Agent", "onefold")
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	c := in.Client
	if c == nil {
		c = &http.Client{}
	}
	resp, err := c.Do(req)
	if err != nil {
		return have, fmt.Errorf("%s: %w", p.Name, err)
	}
	defer resp.Body.Close()
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	switch {
	case resp.StatusCode == http.StatusPartialContent && have > 0:
		if !strings.HasPrefix(resp.Header.Get("Content-Range"), fmt.Sprintf("bytes %d-", have)) {
			return have, fmt.Errorf("%s: unexpected Content-Range %q", p.Name, resp.Header.Get("Content-Range"))
		}
	case resp.StatusCode == http.StatusOK:
		flags |= os.O_TRUNC // server ignored the Range: start over
		h.Reset()
		have = 0
	default:
		return have, fmt.Errorf("%s: %s: HTTP %s", p.Name, p.URL, resp.Status)
	}
	f, err := os.OpenFile(part, flags, statedir.FileMode)
	if err != nil {
		return have, err
	}
	defer f.Close()
	buf := make([]byte, 256<<10)
	last := time.Time{}
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if have+int64(n) > p.Size {
				return have, fmt.Errorf("%s: server sent more than the pinned size %d", p.Name, p.Size)
			}
			if _, err := f.Write(buf[:n]); err != nil {
				return have, err
			}
			h.Write(buf[:n])
			have += int64(n)
			if time.Since(last) > 200*time.Millisecond {
				last = time.Now()
				in.report(Progress{ID: p.ID, Phase: PhaseDownload, Done: have, Total: p.Size})
			}
		}
		if rerr == io.EOF {
			in.report(Progress{ID: p.ID, Phase: PhaseDownload, Done: have, Total: p.Size})
			return have, nil
		}
		if rerr != nil {
			return have, fmt.Errorf("%s: %w", p.Name, rerr) // part kept: the next attempt resumes
		}
	}
}

// member is one archive entry to unpack.
type member struct {
	name string
	open func() (io.ReadCloser, error)
}

// extract writes the members mapped by p.Files under dst and returns the
// written paths (slash-separated, relative to dst). Every mapped file must be
// found: a package missing one is not installed.
func extract(p Package, archive, dst string) ([]string, error) {
	var members []member
	var closer io.Closer
	switch p.Format {
	case "zip":
		zr, err := zip.OpenReader(archive)
		if err != nil {
			return nil, err
		}
		closer = zr
		for _, f := range zr.File {
			members = append(members, member{f.Name, f.Open})
		}
	case "7z-sfx":
		r, c, err := openSFX(archive)
		if err != nil {
			return nil, err
		}
		closer = c
		for _, f := range r.File {
			members = append(members, member{f.Name, f.Open})
		}
	default:
		return nil, fmt.Errorf("unknown format %q", p.Format)
	}
	defer closer.Close()
	var out []string
	for _, m := range p.Files {
		found := false
		for _, e := range members {
			rel, ok := mapMember(m, e.name)
			if !ok {
				continue
			}
			found = true
			if strings.HasSuffix(rel, "/") {
				continue // directory entry
			}
			if err := writeMember(e, filepath.Join(dst, filepath.FromSlash(rel))); err != nil {
				return nil, err
			}
			out = append(out, rel)
		}
		if !found {
			return nil, fmt.Errorf("%s not found in the archive", m.From)
		}
	}
	return out, nil
}

// mapMember returns the target of an archive entry under mapping m. Names
// that would escape the target (absolute, "..") are refused.
func mapMember(m File, name string) (string, bool) {
	name = strings.ReplaceAll(name, `\`, "/")
	if strings.HasSuffix(m.From, "/") {
		if !strings.HasPrefix(name, m.From) || name == m.From {
			return "", false
		}
		rest := strings.TrimPrefix(name, m.From)
		clean := path.Clean(rest)
		if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." || path.IsAbs(clean) {
			return "", false
		}
		if strings.HasSuffix(rest, "/") {
			return m.To + clean + "/", true
		}
		return m.To + clean, true
	}
	if name != m.From {
		return "", false
	}
	return m.To, true
}

func writeMember(e member, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), statedir.DirMode); err != nil {
		return err
	}
	rc, err := e.open()
	if err != nil {
		return fmt.Errorf("%s: %w", e.name, err)
	}
	defer rc.Close()
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, rc); err != nil {
		f.Close()
		return fmt.Errorf("%s: %w", e.name, err)
	}
	return f.Close()
}

// sevenZipSig starts every 7z archive; the 7-Zip installer stub contains the
// same bytes, so each occurrence is tried until one opens.
var sevenZipSig = []byte{'7', 'z', 0xBC, 0xAF, 0x27, 0x1C}

// openSFX opens the 7z archive appended to a self-extracting exe.
func openSFX(file string) (*sevenzip.Reader, io.Closer, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, nil, err
	}
	for off := 0; ; off++ {
		i := bytes.Index(b[off:], sevenZipSig)
		if i < 0 {
			return nil, nil, errors.New("no 7z archive in the installer")
		}
		off += i
		r, err := sevenzip.NewReader(bytes.NewReader(b[off:]), int64(len(b)-off))
		if err == nil {
			return r, io.NopCloser(nil), nil // the archive is in memory: nothing to close
		}
	}
}

// topLevel returns the distinct first path elements of files (with a
// trailing "/" for directories), in order.
func topLevel(files []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		top, _, isDir := strings.Cut(f, "/")
		if isDir {
			top += "/"
		}
		if !seen[top] {
			seen[top] = true
			out = append(out, top)
		}
	}
	return out
}

func (in *Installer) record(p Package, files []string) error {
	m := ReadInstalled(in.Dir)
	m[p.ID] = Installed{Version: p.Version, SHA256: p.SHA256, Files: files, At: time.Now().UTC()}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(in.Dir, manifestName+fsx.TempSuffix)
	if err := os.WriteFile(tmp, b, statedir.FileMode); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(in.Dir, manifestName))
}
