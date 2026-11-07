// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package materialize executes a golden action: it copies the source into the
// canonical golden tree, embeds the canonical timestamp + ownership + signature,
// and crystallizes the mtime.
//
// Silvering matches the two pilot tools 1:1:
//   - photos (jpg, heic/heif/avif, png, webp, tiff) -> exiftool rewrites
//     DateTimeOriginal/CreateDate/ModifyDate to the canonical capture time, plus
//     Artist/Copyright and the Onefold signature (Comment, or UserComment where
//     the format has no Comment). Camera RAW is copied byte-identical.
//   - videos (every container but MPEG-PS, which has no metadata slots and is
//     copied byte-identical) -> ffmpeg -c copy remux stamping creation_time,
//     artist, the signature comment and the stream language. No re-encode: the
//     payload bytes are preserved.
//     AVI goes through ffmpeg too: exiftool can READ the EXIF that some camera AVIs
//     embed (used at extraction time) but CANNOT write RIFF/AVI ("Can't currently
//     write RIFF AVI files"), whereas ffmpeg writes artist+comment into the RIFF
//     INFO chunk. The AVI container has no creation_time / per-stream language slot,
//     so those two are silently dropped by ffmpeg for AVI — the mtime still carries
//     the canonical date.
//
// Every file is built under a onefold temp name next to its destination
// (fsx.TempFor), silvered there, synced, given its mtime, and only then
// published with a no-replace rename: the final name appears complete or not at
// all, and never replaces a file that appeared meanwhile.
//
// SAFETY: it only ever WRITES to the target tree. It never modifies or deletes a
// source file, and it is idempotent (a destination that already exists is skipped
// — silvering changes the byte size, so existence, not size, is the skip test),
// so a run can be re-issued after an interruption.
package materialize

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/meta"
	"github.com/mauriziofonte/onefold/internal/plan"
	"github.com/mauriziofonte/onefold/internal/tools"
)

// toolTimeout bounds one external stamping process (and is the base of the
// remux bound, toolTimeoutFor): a tool that hangs without dying must not
// block the run forever.
var toolTimeout = 30 * time.Minute

// toolTimeoutFor grows the bound with the file size (at least 4 MiB/s on top
// of the base), so a 50 GB video on a slow disk is not mistaken for a hang.
func toolTimeoutFor(size int64) time.Duration {
	return toolTimeout + time.Duration(size/(4<<20))*time.Second
}

// Silver holds the tags embedded into media copies besides the capture time,
// the device and the owner (which travel in the plan). An empty field is not
// written. Callers resolve them from the user's config (internal/config).
type Silver struct {
	Signature string // Comment (photos, AVI) / comment (videos)
	Language  string // ISO 639-2 language of the first video and audio stream
}

// Reasons a media copy is published without canonical metadata.
const (
	NoExiftool = "exiftool not found"
	NoFfmpeg   = "ffmpeg not found"
)

// Result of applying one action.
type Result struct {
	Dst     string
	Skipped bool // already materialized
	// Unsilvered is set when a photo or video was published as a faithful copy
	// without its canonical metadata: the reason (tool missing or failing).
	Unsilvered string
	Cloned     bool // started as a clone of the source (RunOptions.Clone)
}

// Apply materializes a into target/<canonRel> without signature or language.
// For a photo, batch (if non-nil) stamps via a shared exiftool -stay_open
// process, avoiding the per-file spawn; a nil batch (or a batch failure) falls
// back to a one-shot exiftool. Videos always go through ffmpeg (batch is ignored).
//
// NOTE: the target must be a real filesystem. WSL drvfs (/mnt/d) cannot serve
// the mp4 muxer's seek-write nor large sequential writes reliably (I/O errors),
// so on WSL materialize into a native ext4 path and push from there.
func Apply(a plan.Action, target string, batch *ExifBatch) (Result, error) {
	return ApplyWith(a, target, batch, Silver{})
}

// ApplyWith is Apply with explicit silver tags.
func ApplyWith(a plan.Action, target string, batch *ExifBatch, s Silver) (Result, error) {
	return applyWith(a, target, batch, s, false)
}

// applyWith: clone = start each byte copy as a clone of the source where the
// file system allows it (RunOptions.Clone).
func applyWith(a plan.Action, target string, batch *ExifBatch, s Silver, clone bool) (Result, error) {
	dst := filepath.Join(target, filepath.FromSlash(a.CanonRel))
	// Idempotency: silvering (exiftool/ffmpeg) mutates the byte size, so a
	// re-materialized file never matches SrcSize — skip on existence instead.
	if _, err := os.Lstat(dst); err == nil {
		return Result{Dst: dst, Skipped: true}, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return Result{Dst: dst}, err
	}

	var tmp, unsilvered string
	var err error
	var cloned bool
	switch {
	case !a.IsMediaAction():
		// generic kinds: faithful byte copy, no metadata embedding.
		tmp = fsx.TempFor(dst, false)
		cloned, err = copyFile(a.SrcPath, tmp, clone)
	case byteCopyMedia(a.SrcPath):
		// camera RAW (the digital negative) and MPEG-PS (no metadata slots):
		// byte-identical copy by design, not counted as unsilvered.
		tmp = fsx.TempFor(dst, false)
		cloned, err = copyFile(a.SrcPath, tmp, clone)
	case a.Kind == "video":
		// every other video container: remux with ffmpeg (payload copied, metadata stamped).
		tmp, unsilvered, err = remuxVideo(a, dst, s)
	default:
		// photos (jpg, heic, png, webp, tiff...): byte copy, then exiftool stamp on the temp file.
		tmp = fsx.TempFor(dst, false)
		if cloned, err = copyFile(a.SrcPath, tmp, clone); err == nil {
			if unsilvered = stampPhoto(a, tmp, batch, s); unsilvered == "" {
				err = syncFile(tmp) // exiftool rewrote the file and does not sync it
			}
		}
	}
	if err != nil {
		os.Remove(tmp) // our own temp: never left behind by a failed action
		return Result{Dst: dst}, err
	}
	// Crystallize the filesystem mtime to the canonical capture time (a rename
	// keeps it), then publish without ever replacing an existing file.
	if t, ok := parseCanon(a.Time); ok {
		_ = os.Chtimes(tmp, t, t)
	}
	if err := fsx.RenameNoReplace(tmp, dst); err != nil {
		os.Remove(tmp)
		if errors.Is(err, fsx.ErrExists) {
			return Result{Dst: dst, Skipped: true}, nil // appeared meanwhile: never replaced
		}
		return Result{Dst: dst}, err
	}
	return Result{Dst: dst, Unsilvered: unsilvered, Cloned: cloned}, nil
}

// LinkOriginal materializes a as a hard link: target/<canonRel> becomes a
// second name of the source file, the original bytes, with no copy, no
// silvering and the mtime left as it is (a hard link shares it with the
// source: changing it would change the source). Source and target must be on
// the same volume (fsx.ErrCrossVolume otherwise). The source is never
// modified: only a name is added in the target. Idempotent like ApplyWith: an
// existing destination is skipped, never replaced.
func LinkOriginal(a plan.Action, target string) (Result, error) {
	dst := filepath.Join(target, filepath.FromSlash(a.CanonRel))
	if _, err := os.Lstat(dst); err == nil {
		return Result{Dst: dst, Skipped: true}, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return Result{Dst: dst}, err
	}
	if err := fsx.LinkNoReplace(a.SrcPath, dst); err != nil {
		if errors.Is(err, fsx.ErrExists) {
			return Result{Dst: dst, Skipped: true}, nil // appeared meanwhile: never replaced
		}
		return Result{Dst: dst}, err
	}
	return Result{Dst: dst}, nil
}

// CheckLink tells before a -link run whether the target can hold hard links
// of the sources: it links the first action's source to a onefold temp name
// in target and removes that temp name again. It catches the common mistakes
// (target on another volume, a filesystem without hard links) before any
// file is written. Sources on several volumes still fail one by one, each
// with fsx.ErrCrossVolume.
func CheckLink(acts []plan.Action, target string) error {
	if len(acts) == 0 {
		return nil
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	probe := fsx.TempFor(filepath.Join(target, "link-check"), false)
	os.Remove(probe) // a probe left by a killed run: our own temp name
	if err := fsx.LinkNoReplace(acts[0].SrcPath, probe); err != nil {
		return err
	}
	return os.Remove(probe)
}

// parseCanon parses a plan's canonical "2006-01-02 15:04:05" wall-clock time in
// the canonical zone (meta.Zone), whatever the host zone is.
func parseCanon(s string) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, meta.Zone())
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// buildExifArgs is the canonical exiftool argument set (capture date + ownership
// + device + signature), without the target path. Shared by the per-file and the
// stay_open batch stamping paths so both write identical metadata. No -q: the
// batch reads exiftool's per-file summary to tell a refused file.
func buildExifArgs(a plan.Action, s Silver) []string {
	args := []string{"-overwrite_original", "-m"}
	if t, ok := parseCanon(a.Time); ok {
		d := t.Format("2006:01:02 15:04:05")
		args = append(args, "-DateTimeOriginal="+d, "-CreateDate="+d, "-ModifyDate="+d)
	}
	if a.Artist != "" {
		args = append(args, "-Artist="+a.Artist, "-Copyright="+a.Artist)
	}
	if a.CanonDevice != "" {
		// canonical device name, persisted so the next import reads it back stable
		args = append(args, "-XMP-dc:Source="+a.CanonDevice)
	}
	if s.Signature != "" {
		// JPEG (COM segment) and PNG (text chunk) have a Comment; the EXIF-based
		// formats (HEIC/HEIF/AVIF, WebP, TIFF) carry it as UserComment.
		switch kinds.Format(kinds.Ext(a.SrcPath)) {
		case "jpeg", "png":
			args = append(args, "-Comment="+s.Signature)
		default:
			args = append(args, "-UserComment="+s.Signature)
		}
	}
	return args
}

// byteCopyMedia reports the media formats published as byte-identical copies.
func byteCopyMedia(src string) bool {
	ext := kinds.Ext(src)
	return kinds.IsRAW(ext) || kinds.Format(ext) == "mpeg-ps"
}

// stampPhoto stamps path via the shared stay_open batch when available, else a
// one-shot exiftool. It returns "" when stamped, else why the copy stays a
// faithful unsilvered one (exiftool missing, or refusing the file).
func stampPhoto(a plan.Action, path string, batch *ExifBatch, s Silver) string {
	// exiftool -overwrite_original writes <path>_exiftool_tmp first and refuses
	// to start when one exists: a leftover of a killed run is ours to remove.
	os.Remove(path + exiftoolTmp)
	args := append(buildExifArgs(a, s), path)
	if batch != nil {
		// argument files carry UTF-8 names: on Windows exiftool must be told so
		err := batch.Stamp(append([]string{"-charset", "filename=utf8"}, args...))
		if err == nil {
			return ""
		}
		var se *StampError
		if errors.As(err, &se) {
			return "exiftool: " + se.Msg // the file itself was refused: a one-shot would fail too
		}
		// batch process died or hung: fall back to a one-shot for this file.
		os.Remove(path + exiftoolTmp)
	}
	cmd := tools.Command("exiftool", args...)
	if cmd == nil {
		return NoExiftool
	}
	if out, err := runBounded(cmd, toolTimeoutFor(a.SrcSize)); err != nil {
		os.Remove(path + exiftoolTmp)
		return "exiftool: " + firstLine(out, err)
	}
	return ""
}

// exiftoolTmp is the suffix of exiftool's own temp file next to the one it
// rewrites (fsx.IsTemp recognizes it on onefold temps).
const exiftoolTmp = "_exiftool_tmp"

// remuxVideo copies the video payload into a temp file next to dst with ffmpeg
// (-c copy, no re-encode) while stamping creation_time, artist, the signature
// comment and the stream language. The temp keeps the REAL extension (ffmpeg
// picks the muxer from it). Without ffmpeg, or when the remux fails, the temp
// is a faithful byte copy and the reason is returned.
func remuxVideo(a plan.Action, dst string, s Silver) (tmp, unsilvered string, err error) {
	tmp = fsx.TempFor(dst, true)
	args := []string{
		"-loglevel", "error", "-hide_banner", "-err_detect", "ignore_err", "-nostdin", "-y",
		"-i", a.SrcPath, "-map", "0:v?", "-map", "0:a?", "-c", "copy",
	}
	if t, ok := parseCanon(a.Time); ok {
		// explicit UTC: a zoneless value is read in the HOST zone by ffmpeg
		// (same bytes as before on Europe/Rome hosts, right on every other one)
		args = append(args, "-metadata", "creation_time="+t.UTC().Format("2006-01-02T15:04:05Z"))
	}
	if a.Artist != "" {
		args = append(args, "-metadata", "artist="+a.Artist)
	}
	if s.Signature != "" {
		args = append(args, "-metadata", "comment="+s.Signature)
	}
	if s.Language != "" {
		args = append(args, "-metadata:s:a:0", "language="+s.Language, "-metadata:s:v:0", "language="+s.Language)
	}
	args = append(args, tmp)
	cmd := tools.Command("ffmpeg", args...)
	if cmd == nil {
		_, err := copyFile(a.SrcPath, tmp, false)
		return tmp, NoFfmpeg, err // no ffmpeg (Windows): faithful unsilvered copy
	}
	if out, rerr := runBounded(cmd, toolTimeoutFor(a.SrcSize)); rerr != nil {
		os.Remove(tmp)
		// Some exotic codecs can't be remuxed into mp4 with -c copy (e.g. flv1 /
		// Sorenson: "codec not currently supported in container"). Rather than
		// drop the video from the golden, fall back to a raw byte copy: the file
		// is preserved bit-identical (just unsilvered — no embedded metadata).
		_, err := copyFile(a.SrcPath, tmp, false)
		return tmp, "ffmpeg: " + firstLine(out, rerr), err
	}
	return tmp, "", syncFile(tmp)
}

// copyFile copies src into tmp (created or truncated) and syncs it to disk.
// The caller publishes tmp with a no-replace rename.
// copyFile copies src to tmp and flushes it; with clone, tmp is first tried
// as a clone of src (cloned = true), flushed the same way.
func copyFile(src, tmp string, clone bool) (cloned bool, err error) {
	if clone {
		os.Remove(tmp) // a stale temp of an earlier run: a clone needs a free name
		switch cerr := fsx.Clone(src, tmp); {
		case cerr == nil:
			if err := fsx.SyncFile(tmp); err != nil {
				os.Remove(tmp)
				return false, err
			}
			return true, nil
		case !errors.Is(cerr, fsx.ErrNoClone):
			return false, cerr
		}
	}
	in, err := os.Open(src)
	if err != nil {
		return false, err
	}
	defer in.Close()
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return false, err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return false, err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tmp)
		return false, err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return false, err
	}
	return false, nil
}

// syncFile flushes a file written by an external tool.
func syncFile(p string) error {
	f, err := os.OpenFile(p, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// runBounded runs cmd capturing its combined output, killing it (with every
// process it started) after timeout.
func runBounded(cmd *exec.Cmd, timeout time.Duration) ([]byte, error) {
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	cmd.WaitDelay = 10 * time.Second // never wait forever on pipes a grandchild holds
	tree, err := tools.StartTree(cmd)
	if err != nil {
		return nil, err
	}
	defer tree.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return buf.Bytes(), err
	case <-time.After(timeout):
		tree.Kill()
		<-done
		return buf.Bytes(), fmt.Errorf("killed after %s", timeout)
	}
}

// firstLine is the first non-empty output line, else the error text.
func firstLine(out []byte, err error) string {
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return err.Error()
}

// MissingTools lists the tools ("exiftool", "ffmpeg") that silvering needs
// for the actions still to be written under target but that are not
// available: without them those copies would be published unsilvered, and a
// later run would skip them. Generic kinds and byte-copied media need none.
func MissingTools(acts []plan.Action, target string) []string {
	need := map[string]bool{}
	for _, a := range acts {
		if !a.IsMediaAction() || byteCopyMedia(a.SrcPath) {
			continue
		}
		tool := "exiftool"
		if a.Kind == "video" {
			tool = "ffmpeg"
		}
		if need[tool] || tools.Has(tool) {
			continue
		}
		if _, err := os.Lstat(filepath.Join(target, filepath.FromSlash(a.CanonRel))); err == nil {
			continue // already in the target: skipped, nothing to stamp
		}
		need[tool] = true
	}
	var out []string
	for _, t := range []string{"exiftool", "ffmpeg"} {
		if need[t] {
			out = append(out, t)
		}
	}
	return out
}
