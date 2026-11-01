// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadAndDefaults(t *testing.T) {
	t.Setenv("ONEFOLD_STATE_DIR", t.TempDir())
	c, err := Load()
	if err != nil || c != (Config{}) {
		t.Fatalf("missing file must be the zero config: %+v %v", c, err)
	}
	if s := c.EffectiveSignature("v1.0.0"); s != "Onefold v1.0.0" {
		t.Fatalf("default signature %q", s)
	}
	for _, kv := range [][2]string{{"artist", " Jane Doe "}, {"signature", "My archive"}, {"language", "ENG"}, {"diag-log", "on"}, {"photo-date-fallback", "yes"}} {
		if err := c.Set(kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	p, _ := Path()
	if err := SaveTo(p, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	want := Config{Artist: "Jane Doe", Signature: "My archive", Language: "eng", DiagLog: true, PhotoDateFallback: true}
	if err != nil || got != want {
		t.Fatalf("reload %+v %v", got, err)
	}
	if v, _ := got.Get("diag-log"); v != "on" {
		t.Fatalf("diag-log %q", v)
	}
	if v, _ := got.Get("photo-date-fallback"); v != "on" {
		t.Fatalf("photo-date-fallback %q", v)
	}
	got.Set("signature", None)
	if s := got.EffectiveSignature("v1.0.0"); s != "" {
		t.Fatalf("signature %q must be none", s)
	}
	if _, err := os.Stat(p + ".onefold-tmp"); !os.IsNotExist(err) {
		t.Fatal("temp left behind")
	}
}

func TestValidation(t *testing.T) {
	var c Config
	for _, kv := range [][2]string{{"language", "english"}, {"language", "e1g"}, {"artist", "a\nb"}, {"signature", "x\ty"}, {"diag-log", "maybe"}, {"photo-date-fallback", "maybe"}, {"nope", "x"}} {
		if err := c.Set(kv[0], kv[1]); err == nil {
			t.Errorf("set %s=%q accepted", kv[0], kv[1])
		}
	}
	if c != (Config{}) {
		t.Fatalf("a refused set changed the config: %+v", c)
	}
	p := filepath.Join(t.TempDir(), FileName)
	os.WriteFile(p, []byte(`{"language":"english"}`), 0o644)
	if _, err := LoadFrom(p); err == nil {
		t.Fatal("invalid file accepted")
	}
}

func TestOverrideAndSignature(t *testing.T) {
	for _, c := range []struct{ flag, conf, want string }{{"", "conf", "conf"}, {None, "conf", ""}, {"flag", "conf", "flag"}, {"", "", ""}} {
		if got := Override(c.flag, c.conf); got != c.want {
			t.Errorf("Override(%q, %q) = %q", c.flag, c.conf, got)
		}
	}
	for in, want := range map[string]string{"v1.0.0": "Onefold v1.0.0", "1.2.3": "Onefold v1.2.3", "v1.0.0-2-gabc-dirty": "Onefold v1.0.0-2-gabc-dirty", "dev": "Onefold dev", "": "Onefold dev", "f2a5ac7": "Onefold f2a5ac7"} {
		if got := DefaultSignature(in); got != want {
			t.Errorf("DefaultSignature(%q) = %q want %q", in, got, want)
		}
	}
}
