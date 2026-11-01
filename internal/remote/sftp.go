// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package remote is onefold's pure-Go SFTP transport for the materialization
// push (staged winners -> remote golden tree), built on the de facto Go stack:
// golang.org/x/crypto/ssh (protocol, agent), github.com/pkg/sftp (SFTP) and
// github.com/skeema/knownhosts (known_hosts with host-key algorithm
// negotiation, the x/crypto/knownhosts gap). Authentication: private key file
// (passphrase prompted if encrypted) and/or the running ssh-agent. A
// connection test runs BEFORE any bytes are transferred.
package remote

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/pkg/sftp"
	"github.com/skeema/knownhosts"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/term"

	"github.com/mauriziofonte/onefold/internal/fsx"
)

// Config is the SFTP connection configuration.
type Config struct {
	Host       string
	Port       int // 0 -> 22
	User       string
	KeyPath    string // private key; passphrase prompted if encrypted. "" = ssh-agent only
	NoAgent    bool   // do not offer ssh-agent identities
	KnownHosts string // "" -> ~/.ssh/known_hosts
	Insecure   bool   // skip host-key verification
	KeepAlive  time.Duration
}

// Client is a live SSH+SFTP session.
type Client struct {
	ssh    *ssh.Client
	sftp   *sftp.Client
	agent  io.Closer
	stopKA chan struct{}
}

// Dial authenticates and opens an SFTP session.
func Dial(cfg Config) (*Client, error) {
	if cfg.Host == "" || cfg.User == "" {
		return nil, errors.New("host and user are required")
	}
	port := cfg.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(port))
	c := &Client{}
	var auths []ssh.AuthMethod
	if cfg.KeyPath != "" {
		a, err := keyAuth(cfg.KeyPath)
		if err != nil {
			return nil, err
		}
		auths = append(auths, a)
	}
	if !cfg.NoAgent {
		if conn, err := agentConn(); err == nil {
			c.agent = conn
			auths = append(auths, ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
		}
	}
	if len(auths) == 0 {
		return nil, errors.New("no credentials: pass --key or run an ssh-agent with the key loaded")
	}
	cc := &ssh.ClientConfig{User: cfg.User, Auth: auths, Timeout: 15 * time.Second}
	if cfg.Insecure {
		cc.HostKeyCallback = ssh.InsecureIgnoreHostKey()
	} else {
		kh, err := knownHostsPath(cfg.KnownHosts)
		if err != nil {
			return nil, err
		}
		db, err := knownhosts.NewDB(kh)
		if err != nil {
			return nil, fmt.Errorf("known_hosts %s: %w (pass --insecure-host-key to skip)", kh, err)
		}
		cc.HostKeyCallback = db.HostKeyCallback()
		// Ask the server for the key type we actually trust (else it may offer
		// e.g. rsa while known_hosts holds ed25519, and the check fails).
		cc.HostKeyAlgorithms = db.HostKeyAlgorithms(addr)
	}
	sc, err := ssh.Dial("tcp", addr, cc)
	if err != nil {
		c.closeAgent()
		switch {
		case knownhosts.IsHostKeyChanged(err):
			return nil, fmt.Errorf("REMOTE HOST KEY CHANGED for %s (possible man-in-the-middle): %w", addr, err)
		case knownhosts.IsHostUnknown(err):
			return nil, fmt.Errorf("%s is not in known_hosts: connect once with ssh to record its key, or pass --insecure-host-key: %w", addr, err)
		}
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	fc, err := sftp.NewClient(sc, sftp.UseConcurrentWrites(true), sftp.MaxConcurrentRequestsPerFile(64))
	if err != nil {
		sc.Close()
		c.closeAgent()
		return nil, fmt.Errorf("sftp session: %w", err)
	}
	c.ssh, c.sftp = sc, fc
	ka := cfg.KeepAlive
	if ka == 0 {
		ka = 30 * time.Second
	}
	c.stopKA = make(chan struct{})
	go c.keepAlive(ka, c.stopKA)
	return c, nil
}

// keepAlive pings the server so idle NAT/firewall state is never dropped
// mid-transfer; a dead peer closes the connection (pending calls then fail).
// It gets its stop channel as an argument: Close clears c.stopKA, and reading
// the field here would race with that write.
func (c *Client) keepAlive(every time.Duration, stop <-chan struct{}) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if _, _, err := c.ssh.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				c.ssh.Close()
				return
			}
		}
	}
}

func (c *Client) closeAgent() {
	if c.agent != nil {
		c.agent.Close()
	}
}

// Close tears down the SFTP and SSH sessions.
func (c *Client) Close() error {
	if c.stopKA != nil {
		close(c.stopKA)
		c.stopKA = nil
	}
	if c.sftp != nil {
		c.sftp.Close()
	}
	c.closeAgent()
	if c.ssh != nil {
		return c.ssh.Close()
	}
	return nil
}

// agentConn connects to the running ssh-agent: SSH_AUTH_SOCK (Unix, also
// honored on Windows), else the Windows OpenSSH agent named pipe.
func agentConn() (io.ReadWriteCloser, error) {
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		return net.DialTimeout("unix", sock, 5*time.Second)
	}
	if runtime.GOOS == "windows" {
		return os.OpenFile(`\\.\pipe\openssh-ssh-agent`, os.O_RDWR, 0)
	}
	return nil, errors.New("no ssh-agent")
}

func knownHostsPath(p string) (string, error) {
	if p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home for known_hosts: %w", err)
	}
	return filepath.Join(home, ".ssh", "known_hosts"), nil
}

// keyAuth loads the private key, prompting for a passphrase if it is encrypted.
func keyAuth(keyPath string) (ssh.AuthMethod, error) {
	pem, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read key %s: %w", keyPath, err)
	}
	signer, err := ssh.ParsePrivateKey(pem)
	if err == nil {
		return ssh.PublicKeys(signer), nil
	}
	var missing *ssh.PassphraseMissingError
	if !errors.As(err, &missing) {
		return nil, fmt.Errorf("parse key %s: %w", keyPath, err)
	}
	pass, perr := promptPassphrase(keyPath)
	if perr != nil {
		return nil, fmt.Errorf("read passphrase: %w", perr)
	}
	signer, err = ssh.ParsePrivateKeyWithPassphrase(pem, pass)
	if err != nil {
		return nil, fmt.Errorf("decrypt key %s: %w", keyPath, err)
	}
	return ssh.PublicKeys(signer), nil
}

// promptPassphrase reads a passphrase without echo, from /dev/tty when available
// (so it works even if stdin is piped), like ssh's own prompt; on Windows from
// the console via stdin.
func promptPassphrase(keyPath string) ([]byte, error) {
	if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		defer tty.Close()
		fmt.Fprintf(tty, "Enter passphrase for key %s: ", keyPath)
		p, rerr := term.ReadPassword(int(tty.Fd()))
		fmt.Fprintln(tty)
		return p, rerr
	}
	fmt.Fprintf(os.Stderr, "Enter passphrase for key %s: ", keyPath)
	p, rerr := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return p, rerr
}

// TestConnection verifies the session is usable and the remote parent dir is
// reachable, BEFORE any transfer. A missing target dir is fine (it is created on
// upload); any other stat error aborts the run.
func (c *Client) TestConnection(remoteDir string) error {
	if _, err := c.sftp.Getwd(); err != nil {
		return fmt.Errorf("sftp getwd: %w", err)
	}
	if remoteDir != "" {
		if _, err := c.sftp.Stat(remoteDir); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat remote %s: %w", remoteDir, err)
		}
	}
	return nil
}

// Stats is the outcome of an UploadTree.
type Stats struct {
	Uploaded  int
	Skipped   int
	Bytes     int64
	Conflicts []string // relative paths that exist remotely with a DIFFERENT size: never overwritten
}

// UploadTree mirrors localDir into remoteDir over SFTP. Idempotent: a remote file
// that already exists with the same size is skipped, so a failed run is resumed
// by running it again. A remote file with a different size is a conflict: it is
// left untouched and listed in Stats.Conflicts (the remote golden tree is
// append-only). Each file is written to a .part sibling, size-checked,
// given the local mtime, then renamed, so an interrupted transfer never leaves a
// partial file at the final path. onFile (if set) is called once per file with
// its size, upload duration and whether it was skipped.
func (c *Client) UploadTree(localDir, remoteDir string, onFile func(rel string, bytes int64, dur time.Duration, skipped bool)) (Stats, error) {
	var st Stats
	lroot := filepath.Clean(localDir)
	err := filepath.Walk(lroot, func(p string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		rel, rerr := filepath.Rel(lroot, p)
		if rerr != nil {
			return rerr
		}
		rpath := path.Join(remoteDir, filepath.ToSlash(rel))
		if info.IsDir() {
			return c.sftp.MkdirAll(rpath)
		}
		if fsx.IsTemp(info.Name()) {
			return nil // in-flight file of an interrupted apply/export: never shipped
		}
		if fi, serr := c.sftp.Stat(rpath); serr == nil {
			if fi.Size() == info.Size() {
				st.Skipped++
			} else {
				st.Conflicts = append(st.Conflicts, filepath.ToSlash(rel))
			}
			if onFile != nil {
				onFile(rel, info.Size(), 0, true)
			}
			return nil
		}
		t0 := time.Now()
		n, uerr := c.uploadFile(p, rpath, info)
		if uerr != nil {
			return fmt.Errorf("%s: %w", rel, uerr)
		}
		st.Uploaded++
		st.Bytes += n
		if onFile != nil {
			onFile(rel, n, time.Since(t0), false)
		}
		return nil
	})
	return st, err
}

// TreeBytes sums the sizes of all regular files under dir, for the push total.
func TreeBytes(dir string) (int64, error) {
	var total int64
	err := filepath.Walk(dir, func(_ string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if !info.IsDir() && !fsx.IsTemp(info.Name()) {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// uploadFile streams local -> remote via a .part temp (concurrent SFTP writes),
// checks the remote size, sets the local mtime, then renames atomically.
func (c *Client) uploadFile(local, remote string, info os.FileInfo) (int64, error) {
	in, err := os.Open(local)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	if err := c.sftp.MkdirAll(path.Dir(remote)); err != nil {
		return 0, err
	}
	tmp := remote + ".part"
	out, err := c.sftp.Create(tmp)
	if err != nil {
		return 0, err
	}
	n, err := out.ReadFrom(in)
	if err == nil {
		err = out.Close()
	} else {
		out.Close()
	}
	if err == nil {
		var fi os.FileInfo
		if fi, err = c.sftp.Stat(tmp); err == nil && (fi.Size() != info.Size() || n != info.Size()) {
			err = fmt.Errorf("size mismatch after upload: local %d, sent %d, remote %d", info.Size(), n, fi.Size())
		}
	}
	if err == nil {
		err = c.sftp.Chtimes(tmp, info.ModTime(), info.ModTime())
	}
	if err != nil {
		c.sftp.Remove(tmp)
		return 0, err
	}
	// Plain SFTP rename: OpenSSH refuses an existing target (link+unlink), so a
	// file that appeared meanwhile is never replaced. The golden is append-only.
	if err := c.sftp.Rename(tmp, remote); err != nil {
		c.sftp.Remove(tmp)
		return 0, err
	}
	return n, nil
}
