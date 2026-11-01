// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package purge

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mauriziofonte/onefold/internal/fsx"
)

const trashSupported = true

// The Trash follows the freedesktop.org Trash specification 1.0, the layout
// GNOME, KDE, Xfce, Cinnamon, MATE, LXQt and trash-cli read and restore from.
// It is plain files, so it works the same on every distribution, libc and
// architecture, with or without a desktop.
//
//   - a file on the home volume goes to $XDG_DATA_HOME/Trash
//     (~/.local/share/Trash);
//   - a file on another volume goes to $topdir/.Trash/$uid when the
//     administrator made $topdir/.Trash (a directory, sticky, not a link),
//     else to $topdir/.Trash-$uid, created when missing.
//
// Each item is a rename inside one volume, never a copy: where no Trash can
// be used the move fails and the file stays where it is.

// moveToTrash sends one file or empty directory to the Trash of its volume.
func moveToTrash(p string) error {
	_, err := trashItem(p)
	return err
}

// trashItem moves p to the Trash and returns where it landed.
func trashItem(p string) (string, error) {
	p, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	var st syscall.Stat_t
	if err := syscall.Lstat(p, &st); err != nil {
		return "", &os.PathError{Op: "trash", Path: p, Err: err}
	}
	home, err := homeTrash()
	if err != nil {
		return "", err
	}
	if dev, err := devOf(existingParent(home)); err == nil && dev == uint64(st.Dev) {
		if err := mkTrash(home); err != nil {
			return "", err
		}
		return trashInto(p, home, "")
	}
	top, err := topDir(p, uint64(st.Dev))
	if err != nil {
		return "", err
	}
	trash, err := topTrash(top)
	if err != nil {
		return "", err
	}
	return trashInto(p, trash, top)
}

// homeTrash is $XDG_DATA_HOME/Trash, ~/.local/share/Trash by default.
func homeTrash() (string, error) {
	if d := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(d) {
		return filepath.Join(d, "Trash"), nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("trash: no home directory: %w", err)
	}
	return filepath.Join(h, ".local", "share", "Trash"), nil
}

// topTrash picks the Trash of the volume mounted at top.
func topTrash(top string) (string, error) {
	uid := strconv.Itoa(os.Getuid())
	shared := filepath.Join(top, ".Trash")
	if fi, err := os.Lstat(shared); err == nil && fi.IsDir() && fi.Mode()&os.ModeSticky != 0 {
		t := filepath.Join(shared, uid)
		if mkTrash(t) == nil {
			return t, nil
		}
	}
	t := filepath.Join(top, ".Trash-"+uid)
	if err := mkTrash(t); err != nil {
		return "", fmt.Errorf("trash: the volume at %s has no usable Trash (%v): file kept", top, err)
	}
	return t, nil
}

// mkTrash makes the files and info folders of a Trash (owner only) and
// refuses one whose folders are symbolic links or files.
func mkTrash(t string) error {
	for _, d := range []string{t, filepath.Join(t, "files"), filepath.Join(t, "info")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
		if fi, err := os.Lstat(d); err != nil {
			return err
		} else if !fi.IsDir() {
			return fmt.Errorf("%s is not a folder", d)
		}
	}
	return nil
}

// trashInto reserves a name with an info file (O_EXCL, as the specification
// asks), then renames p into files/. top is the volume root for a volume
// Trash, whose info files hold paths relative to it; "" for the home Trash.
func trashInto(p, trash, top string) (string, error) {
	orig := p
	if top != "" {
		if rel, err := filepath.Rel(top, p); err == nil && !strings.HasPrefix(rel, "..") {
			orig = rel
		}
	}
	info := "[Trash Info]\nPath=" + (&url.URL{Path: orig}).EscapedPath() +
		"\nDeletionDate=" + time.Now().Format("2006-01-02T15:04:05") + "\n"
	base := filepath.Base(p)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; i < 10000; i++ {
		name := base
		if i > 1 {
			name = stem + "." + strconv.Itoa(i) + ext
		}
		ip := filepath.Join(trash, "info", name+".trashinfo")
		f, err := os.OpenFile(ip, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("trash: %w", err)
		}
		_, werr := f.WriteString(info)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		dst := filepath.Join(trash, "files", name)
		if werr == nil {
			werr = fsx.RenameNoReplace(p, dst)
		}
		if errors.Is(werr, fsx.ErrExists) {
			os.Remove(ip)
			continue
		}
		if werr != nil {
			os.Remove(ip)
			return "", fmt.Errorf("trash: %w: file kept", werr)
		}
		return dst, nil
	}
	return "", errors.New("trash: no free name: file kept")
}

// topDir is the mount point of the volume holding p: the highest folder
// above it on the same device.
func topDir(p string, dev uint64) (string, error) {
	d := filepath.Dir(p)
	for {
		up := filepath.Dir(d)
		if up == d {
			return d, nil
		}
		ud, err := devOf(up)
		if err != nil {
			return "", fmt.Errorf("trash: %w", err)
		}
		if ud != dev {
			return d, nil
		}
		d = up
	}
}

func devOf(p string) (uint64, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(p, &st); err != nil {
		return 0, &os.PathError{Op: "stat", Path: p, Err: err}
	}
	return uint64(st.Dev), nil
}

// existingParent is p or its nearest existing ancestor.
func existingParent(p string) string {
	for {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		up := filepath.Dir(p)
		if up == p {
			return p
		}
		p = up
	}
}

// trashVolume: every volume passes the check; one without a usable Trash
// fails per file and keeps it.
func trashVolume(string) (vol string, unc bool, driveType uint32) { return "", false, driveFixed }
