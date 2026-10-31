// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"github.com/mauriziofonte/onefold/internal/tooldl"
	"github.com/mauriziofonte/onefold/internal/tools"
)

// cmdTools lists the downloadable tool packages or installs them:
//
//	onefold tools [list]
//	onefold tools install all|ID...
func cmdTools(args []string) {
	if len(args) == 0 || args[0] == "list" {
		toolsList()
		return
	}
	if args[0] != "install" {
		fatal(2, fmt.Errorf("tools: unknown subcommand %q (list | install all|ID...)", args[0]))
	}
	cat := tooldl.Catalog()
	if len(cat) == 0 {
		fatal(1, fmt.Errorf("tools: nothing downloadable on %s/%s: install exiftool, ffmpeg, poppler-utils and 7zip with the system package manager", runtime.GOOS, runtime.GOARCH))
	}
	ids := args[1:]
	if len(ids) == 0 {
		fatal(2, fmt.Errorf("tools install: name the packages (all, or one or more of: %s)", strings.Join(packageIDs(cat), " ")))
	}
	var pkgs []tooldl.Package
	if len(ids) == 1 && ids[0] == "all" {
		pkgs = cat
	} else {
		for _, id := range ids {
			p, ok := tooldl.Find(id)
			if !ok {
				fatal(2, fmt.Errorf("tools install: unknown package %q (known: %s)", id, strings.Join(packageIDs(cat), " ")))
			}
			pkgs = append(pkgs, p)
		}
	}
	dir := tools.ManagedDir()
	if dir == "" {
		fatal(1, fmt.Errorf("tools: cannot resolve the state directory"))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	in := &tooldl.Installer{Dir: dir, OnProgress: func(p tooldl.Progress) {
		switch p.Phase {
		case tooldl.PhaseDownload:
			fmt.Fprintf(os.Stderr, "\r  %-9s download %s / %s   ", p.ID, mb(p.Done), mb(p.Total))
		case tooldl.PhaseExtract:
			fmt.Fprintf(os.Stderr, "\r  %-9s unpacking%30s", p.ID, "")
		}
	}}
	failed := 0
	for _, p := range pkgs {
		t0 := time.Now()
		if err := in.Install(ctx, p); err != nil {
			fmt.Fprintf(os.Stderr, "\r  %-9s FAILED: %v\n", p.ID, err)
			failed++
			if ctx.Err() != nil {
				break // interrupted: the partial download resumes next time
			}
			continue
		}
		fmt.Fprintf(os.Stderr, "\r  %-9s %s installed (%s, %s)%20s\n", p.ID, p.Version, mb(p.Size), time.Since(t0).Round(time.Second), "")
	}
	tools.Reset()
	fmt.Fprintf(os.Stderr, "tools in %s\n", dir)
	if failed > 0 {
		os.Exit(1)
	}
}

func toolsList() {
	cat := tooldl.Catalog()
	dir := tools.ManagedDir()
	inst := tooldl.ReadInstalled(dir)
	if len(cat) == 0 {
		fmt.Printf("nothing downloadable on %s/%s: use the system package manager (exiftool, ffmpeg, poppler-utils, 7zip)\n", runtime.GOOS, runtime.GOARCH)
	}
	for _, p := range cat {
		state := "not installed"
		if i, ok := inst[p.ID]; ok {
			state = "installed " + i.Version
		}
		fmt.Printf("  %-9s %-8s %9s  %-18s %s  (%s)\n", p.ID, p.Version, mb(p.Size), strings.Join(p.Provides, ","), state, p.Home)
	}
	if len(cat) > 0 {
		fmt.Printf("install: onefold tools install all|ID...  -> %s\n", dir)
	}
}

func packageIDs(cat []tooldl.Package) []string {
	var ids []string
	for _, p := range cat {
		ids = append(ids, p.ID)
	}
	return ids
}

func mb(n int64) string { return fmt.Sprintf("%.1f MB", float64(n)/(1<<20)) }
