// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/mauriziofonte/onefold/internal/config"
	"github.com/mauriziofonte/onefold/internal/meta"
)

// loadConfig reads the user's config; a broken file stops the command (a
// silently ignored owner or signature would end up in every golden file).
func loadConfig() config.Config {
	c, err := config.Load()
	if err != nil {
		fatal(1, fmt.Errorf("config: %w (fix it with `onefold config` or remove the file)", err))
	}
	return c
}

// cmdConfig: onefold config [list] | get KEY | set KEY VALUE | unset KEY | path
func cmdConfig(args []string) {
	p, err := config.Path()
	if err != nil {
		fatal(1, err)
	}
	if len(args) == 0 || args[0] == "list" {
		c := loadConfig()
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, k := range config.Keys {
			v, _ := c.Get(k)
			fmt.Fprintf(tw, "%s\t%s\t%s\n", k, show(v), effect(c, k))
		}
		tw.Flush()
		fmt.Printf("(%s)\n", p)
		return
	}
	switch {
	case args[0] == "path" && len(args) == 1:
		fmt.Println(p)
	case args[0] == "get" && len(args) == 2:
		v, err := loadConfig().Get(args[1])
		if err != nil {
			fatal(2, err)
		}
		fmt.Println(v)
	case (args[0] == "set" && len(args) == 3) || (args[0] == "unset" && len(args) == 2):
		c := loadConfig()
		v := ""
		if args[0] == "set" {
			v = args[2]
		}
		if err := c.Set(args[1], v); err != nil {
			fatal(2, err)
		}
		if err := config.SaveTo(p, c); err != nil {
			fatal(1, err)
		}
		v, _ = c.Get(args[1])
		fmt.Printf("%s = %s  (%s)\n", args[1], show(v), effect(c, args[1]))
	default:
		fmt.Fprintln(os.Stderr, "usage: onefold config [list] | get KEY | set KEY VALUE | unset KEY | path\nkeys: "+strings.Join(config.Keys, ", "))
		os.Exit(2)
	}
}

func show(v string) string {
	if v == "" {
		return "(unset)"
	}
	return v
}

// effect says what a key does with its current value.
func effect(c config.Config, key string) string {
	switch key {
	case "artist":
		if c.Artist == "" {
			return "no Artist/Copyright on photo/video copies"
		}
		return "Artist/Copyright on photo/video copies (plan)"
	case "signature":
		if s := c.EffectiveSignature(version); s != "" {
			return fmt.Sprintf("comment %q on photo/video copies (apply)", s)
		}
		return "no comment on photo/video copies"
	case "language":
		if c.Language == "" {
			return "stream language left as in the source"
		}
		return "language of the first video/audio stream (apply)"
	case "timezone":
		l, src, err := meta.ResolveZone(c.Timezone)
		if err != nil {
			return err.Error()
		}
		if src == "config" {
			return "canonical zone of every name and date"
		}
		return fmt.Sprintf("unset: %s from %s", l, src)
	case "date-order":
		switch c.DateOrder {
		case meta.DMY:
			return "ambiguous XX-XX-YYYY file-name dates read as day-month-year"
		case meta.MDY:
			return "ambiguous XX-XX-YYYY file-name dates read as month-day-year"
		}
		return "ambiguous XX-XX-YYYY file-name dates are not used (unambiguous ones are)"
	case "names-language":
		if c.NamesLanguage == "" {
			return "month names in English ({month-name})"
		}
		return "language of month names ({month-name})"
	case "photo-date-fallback":
		if c.PhotoDateFallback {
			return "a JPEG without DateTimeOriginal takes its EXIF CreateDate, else ModifyDate (catalog; changes its golden name)"
		}
		return "a JPEG without DateTimeOriginal is dated by its file name, else its file time"
	case "keep-awake":
		if c.NoKeepAwake {
			return "the computer may sleep on idle during a long run"
		}
		return "the computer does not sleep on idle while a command or a GUI job works (the display may turn off)"
	case "low-priority":
		if c.LowPriority {
			return "commands and GUI jobs run with low disk and CPU priority (slower, the computer stays responsive)"
		}
		return "normal disk and CPU priority"
	case "diag-log":
		return "Windows only: onefold-gui.log next to the exe"
	}
	return ""
}
