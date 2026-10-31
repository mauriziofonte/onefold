// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package seed writes the commands that copy a materialized golden tree
// (apply --execute into a local staging folder) to its destination in bulk
// with rsync or robocopy: the first seed of a remote tree over a fast link,
// where push (SFTP, one file at a time) is slow. onefold runs neither tool
// (no external transport binaries): the person runs the script. Only the
// files of the plan are listed, and both commands are told never to replace
// a file that exists at the destination.
package seed

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mauriziofonte/onefold/internal/plan"
)

// Result of a Script.
type Result struct {
	Files   int      `json:"files"`
	Bytes   int64    `json:"bytes"`
	Missing []string `json:"missing,omitempty"` // canonRel not in the staging tree (not materialized yet)
}

// Collect returns the canonRels of acts present under staging, sorted, and
// the ones missing.
func Collect(acts []plan.Action, staging, onlyHost string) ([]string, *Result) {
	res := &Result{}
	seen := map[string]bool{}
	var rels []string
	for _, a := range acts {
		if onlyHost != "" && a.SrcHost != onlyHost || seen[a.CanonRel] {
			continue
		}
		seen[a.CanonRel] = true
		fi, err := os.Stat(filepath.Join(staging, filepath.FromSlash(a.CanonRel)))
		if err != nil || !fi.Mode().IsRegular() {
			res.Missing = append(res.Missing, a.CanonRel)
			continue
		}
		rels = append(rels, a.CanonRel)
		res.Files++
		res.Bytes += fi.Size()
	}
	sort.Strings(rels)
	sort.Strings(res.Missing)
	return rels, res
}

// Rsync writes the file list (one canonRel per line) to list and the sh
// command that copies them from staging to dest (a local path or
// user@host:path) without replacing any existing file.
func Rsync(script, list io.Writer, rels []string, staging, dest, listPath string) error {
	for _, r := range rels {
		if strings.ContainsAny(r, "\n\r") {
			return fmt.Errorf("%q: a name with a line break cannot go in a file list", r)
		}
		if _, err := io.WriteString(list, r+"\n"); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(script, "#!/bin/sh\n# onefold seed: copy the plan's files from the staging tree; existing files are never replaced\nset -e\n"+
		"rsync -a --ignore-existing --partial --info=progress2 --files-from=%s -- %s %s\n",
		shq(listPath), shq(strings.TrimSuffix(staging, "/")+"/"), shq(strings.TrimSuffix(dest, "/")+"/"))
	return err
}

// Robocopy writes cmd lines: one robocopy per folder (in chunks under the
// command-line limit) with the folder's file names; /XC /XN /XO exclude every
// file that exists at the destination, so nothing is replaced.
func Robocopy(script io.Writer, rels []string, staging, dest string) error {
	byDir := map[string][]string{}
	var dirs []string
	for _, r := range rels {
		d, f := path.Split(r)
		if strings.ContainsAny(f, "\"\n\r") {
			return fmt.Errorf("%q: a name robocopy cannot take on a command line", r)
		}
		if _, ok := byDir[d]; !ok {
			dirs = append(dirs, d)
		}
		byDir[d] = append(byDir[d], f)
	}
	sort.Strings(dirs)
	var b strings.Builder
	b.WriteString("@echo off\r\nrem onefold seed: copy the plan's files from the staging tree; existing files are never replaced\r\n")
	// no trailing backslash: before the closing quote it would escape it
	win := func(root, d string) string {
		root = strings.TrimRight(root, `\/`)
		if d == "" {
			return root
		}
		return root + `\` + strings.ReplaceAll(strings.TrimSuffix(d, "/"), "/", `\`)
	}
	const maxLine = 7000 // cmd.exe takes 8191 characters
	for _, d := range dirs {
		head := fmt.Sprintf(`robocopy "%s" "%s"`, win(staging, d), win(dest, d))
		tail := " /XC /XN /XO /R:2 /W:5 /NP /NJH\r\nif errorlevel 8 exit /b 1\r\n"
		line := head
		n := 0
		for _, f := range byDir[d] {
			add := ` "` + f + `"`
			if n > 0 && len(line)+len(add)+len(tail) > maxLine {
				b.WriteString(line + tail)
				line, n = head, 0
			}
			line += add
			n++
		}
		b.WriteString(line + tail)
	}
	_, err := io.WriteString(script, b.String())
	return err
}

// shq quotes s for sh.
func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
