// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package remote

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"github.com/skeema/knownhosts"
	"golang.org/x/crypto/ssh"
)

func signer(t *testing.T, k any) ssh.Signer {
	t.Helper()
	s, err := ssh.NewSignerFromKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// server starts an in-process SSH server with an SFTP subsystem over the real
// filesystem, accepting only clientPub. Returns host and port.
func server(t *testing.T, clientPub ssh.PublicKey, hostKeys ...ssh.Signer) (string, int) {
	t.Helper()
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if string(k.Marshal()) == string(clientPub.Marshal()) {
			return nil, nil
		}
		return nil, os.ErrPermission
	}}
	for _, k := range hostKeys {
		cfg.AddHostKey(k)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nch := range chans {
					ch, creqs, err := nch.Accept()
					if err != nil {
						continue
					}
					go func() {
						for r := range creqs {
							ok := r.Type == "subsystem" && string(r.Payload[4:]) == "sftp"
							r.Reply(ok, nil)
							if ok {
								if s, err := sftp.NewServer(ch); err == nil {
									s.Serve()
								}
								ch.Close()
							}
						}
					}()
				}
			}()
		}
	}()
	a := ln.Addr().(*net.TCPAddr)
	return a.IP.String(), a.Port
}

func TestPushEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("in-process SFTP server serves POSIX paths")
	}
	dir := t.TempDir()
	// client key file (test-generated)
	_, cpriv, _ := ed25519.GenerateKey(rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(cpriv)
	keyPath := filepath.Join(dir, "id_test")
	os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600)
	cs := signer(t, cpriv)

	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	rsaHost, ecHost := signer(t, rsaKey), signer(t, ecKey)
	host, port := server(t, cs.PublicKey(), rsaHost, ecHost)
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	kh := filepath.Join(dir, "known_hosts")
	os.WriteFile(kh, nil, 0o600)
	cfg := Config{Host: host, Port: port, User: "u", KeyPath: keyPath, NoAgent: true, KnownHosts: kh}
	if _, err := Dial(cfg); err == nil || !strings.Contains(err.Error(), "not in known_hosts") {
		t.Fatalf("unknown host must be refused: %v", err)
	}
	// known_hosts holds ONLY the rsa key while the client would prefer ecdsa:
	// the negotiation must ask for rsa (the x/crypto/knownhosts pitfall).
	os.WriteFile(kh, []byte(knownhosts.Line([]string{addr}, rsaHost.PublicKey())+"\n"), 0o600)
	cl, err := Dial(cfg)
	if err != nil {
		t.Fatalf("dial with rsa-only known_hosts: %v", err)
	}
	src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
	mt := time.Date(2019, 5, 6, 7, 8, 9, 0, time.UTC)
	for rel, body := range map[string]string{"PICTURES/a.jpg": strings.Repeat("x", 300000), "VIDEOS/2020/b.mp4": "bb", "c.txt": ""} {
		p := filepath.Join(src, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
		os.Chtimes(p, mt, mt)
	}
	if err := cl.TestConnection(dst); err != nil {
		t.Fatal(err)
	}
	st, err := cl.UploadTree(src, dst, nil)
	if err != nil || st.Uploaded != 3 {
		t.Fatalf("upload: %+v %v", st, err)
	}
	for _, rel := range []string{"PICTURES/a.jpg", "VIDEOS/2020/b.mp4", "c.txt"} {
		a, _ := os.ReadFile(filepath.Join(src, rel))
		b, err := os.ReadFile(filepath.Join(dst, rel))
		fi, _ := os.Stat(filepath.Join(dst, rel))
		if err != nil || string(a) != string(b) || !fi.ModTime().Equal(mt) {
			t.Errorf("%s: content or mtime differs (%v)", rel, err)
		}
		if _, err := os.Stat(filepath.Join(dst, rel+".part")); err == nil {
			t.Errorf("%s: .part left behind", rel)
		}
	}
	st, err = cl.UploadTree(src, dst, nil)
	if err != nil || st.Uploaded != 0 || st.Skipped != 3 {
		t.Fatalf("re-run must skip everything: %+v %v", st, err)
	}
	// a different local file under an existing remote name: conflict, never overwritten
	os.WriteFile(filepath.Join(src, "VIDEOS/2020/b.mp4"), []byte("bigger"), 0o644)
	st, err = cl.UploadTree(src, dst, nil)
	if err != nil || st.Uploaded != 0 || len(st.Conflicts) != 1 || st.Conflicts[0] != "VIDEOS/2020/b.mp4" {
		t.Fatalf("size conflict: %+v %v", st, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "VIDEOS/2020/b.mp4")); string(b) != "bb" {
		t.Fatalf("remote file overwritten: %q", b)
	}
	cl.Close()

	// a different key for the same address = changed host key
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	os.WriteFile(kh, []byte(knownhosts.Line([]string{addr}, signer(t, other).PublicKey())+"\n"), 0o600)
	if _, err := Dial(cfg); err == nil || !strings.Contains(err.Error(), "HOST KEY CHANGED") {
		t.Fatalf("changed host key must be refused: %v", err)
	}
}
