// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package config is the per-user configuration of onefold, shared by the CLI
// (`onefold config`) and the GUI (Config view): what silvering embeds into
// photo and video copies, the canonical time zone, how file-name dates and
// month names are read and written and, on Windows, the diagnostic log. It lives in
// <state dir>/config.json. Every field is optional: nothing about the user is
// embedded unless configured.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/meta"
	"github.com/mauriziofonte/onefold/internal/naming"
	"github.com/mauriziofonte/onefold/internal/statedir"
)

// FileName is the config file inside the state dir.
const FileName = "config.json"

// None, as a configured signature or as a command-line value of -artist,
// -signature or -lang, means "do not write this tag".
const None = "-"

// Config is the user's configuration. The zero value is valid: no owner, the
// default signature (DefaultSignature), no stream language, no diagnostic log.
type Config struct {
	Artist    string `json:"artist,omitempty"`    // Artist/Copyright on photo/video copies ("" = none)
	Signature string `json:"signature,omitempty"` // Comment tag ("" = DefaultSignature, None = none)
	Language  string `json:"language,omitempty"`  // ISO 639-2 language of the first video/audio stream ("" = none)
	DiagLog   bool   `json:"diagLog,omitempty"`   // Windows: append launch/GUI diagnostics to onefold-gui.log
	// Timezone is the canonical zone of every name and date (IANA; "" =
	// $TZ, else the host zone).
	Timezone string `json:"timezone,omitempty"`
	// DateOrder reads ambiguous XX-XX-YYYY file-name dates ("dmy", "mdy"; "" =
	// such dates are not used).
	DateOrder string `json:"dateOrder,omitempty"`
	// NamesLanguage is the language of month names in folder and file names
	// ("en", "it"; "" = en).
	NamesLanguage string `json:"namesLanguage,omitempty"`
	// PhotoDateFallback: a JPEG without DateTimeOriginal takes its EXIF
	// CreateDate, else ModifyDate (off = file name, then file time).
	PhotoDateFallback bool `json:"photoDateFallback,omitempty"`
}

// Keys are the names `onefold config` accepts, in display order.
var Keys = []string{"artist", "signature", "language", "timezone", "date-order", "names-language", "photo-date-fallback", "diag-log"}

// Path is <state dir>/config.json.
func Path() (string, error) {
	d, err := statedir.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, FileName), nil
}

// Load reads the config at Path.
func Load() (Config, error) {
	p, err := Path()
	if err != nil {
		return Config{}, err
	}
	return LoadFrom(p)
}

// LoadFrom reads a config file; a missing file is the zero Config.
func LoadFrom(p string) (Config, error) {
	var c Config
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("%s: %w", p, err)
	}
	return c, c.Validate()
}

// SaveTo validates c and writes it to p atomically (temp + rename).
func SaveTo(p string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + fsx.TempSuffix
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, p); err != nil { // replacing the previous config is the point
		os.Remove(tmp)
		return err
	}
	return nil
}

var reLang = regexp.MustCompile(`^[a-z]{3}$`)

// Validate rejects values that cannot travel as a tag: control characters
// (an exiftool argument file is line based) and a language that is not a
// three-letter ISO 639-2 code.
func (c Config) Validate() error {
	for _, f := range [][2]string{{"artist", c.Artist}, {"signature", c.Signature}} {
		if strings.IndexFunc(f[1], unicode.IsControl) >= 0 {
			return fmt.Errorf("%s: control characters are not allowed", f[0])
		}
	}
	if c.Language != "" && !reLang.MatchString(c.Language) {
		return fmt.Errorf("language %q: use a three-letter ISO 639-2 code (eng, ita, deu...)", c.Language)
	}
	if c.Timezone != "" {
		if _, err := meta.LoadZone(c.Timezone); err != nil {
			return fmt.Errorf("timezone: %w", err)
		}
	}
	if c.DateOrder != "" && c.DateOrder != meta.DMY && c.DateOrder != meta.MDY {
		return fmt.Errorf("date-order %q: use dmy or mdy (or unset it)", c.DateOrder)
	}
	if c.NamesLanguage != "" && !naming.IsNamesLanguage(c.NamesLanguage) {
		return fmt.Errorf("names-language %q: use en or it", c.NamesLanguage)
	}
	return nil
}

// Apply makes the process-wide settings of c current: canonical zone,
// file-name date order, language of month names. The CLI calls it
// at start, the GUI before every job. An unknown zone is an error only here:
// commands that never touch a time still run.
func (c Config) Apply() error {
	meta.SetDateOrder(c.DateOrder)
	naming.SetNamesLanguage(c.NamesLanguage)
	meta.SetPhotoDateFallback(c.PhotoDateFallback)
	l, _, err := meta.ResolveZone(c.Timezone)
	if err != nil {
		return err
	}
	meta.SetZone(l)
	return nil
}

// DefaultSignature is the comment stamped when none is configured:
// "Onefold v1.0.0" for a release build ("v" added once), "Onefold dev" else.
func DefaultSignature(version string) string {
	v := strings.TrimPrefix(version, "v")
	if v != "" && v[0] >= '0' && v[0] <= '9' {
		return "Onefold v" + v
	}
	if v == "" {
		v = "dev"
	}
	return "Onefold " + v
}

// EffectiveSignature is the comment a copy gets with this config ("" = none).
func (c Config) EffectiveSignature(version string) string {
	switch c.Signature {
	case "":
		return DefaultSignature(version)
	case None:
		return ""
	}
	return c.Signature
}

// Override applies a command-line value over a configured one: "" keeps the
// configured value, None clears it, anything else replaces it.
func Override(flag, configured string) string {
	switch flag {
	case "":
		return configured
	case None:
		return ""
	}
	return flag
}

// Get returns the stored value of key (diag-log: "on" or "off").
func (c Config) Get(key string) (string, error) {
	switch key {
	case "artist":
		return c.Artist, nil
	case "signature":
		return c.Signature, nil
	case "language":
		return c.Language, nil
	case "timezone":
		return c.Timezone, nil
	case "date-order":
		return c.DateOrder, nil
	case "names-language":
		return c.NamesLanguage, nil
	case "diag-log":
		return onOff(c.DiagLog), nil
	case "photo-date-fallback":
		return onOff(c.PhotoDateFallback), nil
	}
	return "", unknownKey(key)
}

// Set changes one key (Unset = Set to ""), validating the result.
func (c *Config) Set(key, value string) error {
	n := *c
	switch key {
	case "artist":
		n.Artist = strings.TrimSpace(value)
	case "signature":
		n.Signature = strings.TrimSpace(value)
	case "language":
		n.Language = strings.ToLower(strings.TrimSpace(value))
	case "timezone":
		n.Timezone = strings.TrimSpace(value)
	case "date-order":
		n.DateOrder = strings.ToLower(strings.TrimSpace(value))
	case "names-language":
		n.NamesLanguage = strings.ToLower(strings.TrimSpace(value))
	case "diag-log", "photo-date-fallback":
		var on bool
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "on", "true", "1", "yes":
			on = true
		case "off", "false", "0", "no", "":
		default:
			return fmt.Errorf("%s: use on or off", key)
		}
		if key == "diag-log" {
			n.DiagLog = on
		} else {
			n.PhotoDateFallback = on
		}
	default:
		return unknownKey(key)
	}
	if err := n.Validate(); err != nil {
		return err
	}
	*c = n
	return nil
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func unknownKey(key string) error {
	ks := append([]string(nil), Keys...)
	sort.Strings(ks)
	return fmt.Errorf("unknown key %q (keys: %s)", key, strings.Join(ks, ", "))
}
