// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mauriziofonte/onefold/internal/config"
	"github.com/mauriziofonte/onefold/internal/report"
	"github.com/mauriziofonte/onefold/internal/tools"
)

// Logf receives diagnostic lines of the GUI start (the Windows sidecar log of
// cmd/onefold); silent by default.
var Logf = func(format string, a ...any) {}

// DefaultPort is fixed so the page origin (and its localStorage settings)
// stays the same across launches; a busy port falls back to a random one.
const DefaultPort = 47115

const cookieName = "onefold_session"

// Options configure a Server.
type Options struct {
	Port     int    // 0 = DefaultPort (falls back to a random port when busy); -1 = random
	StateDir string // "" = StateDir()
	Version  string
	Build    string // commit and date of the binary ("" = unknown)
	Static   fs.FS  // frontend files ("" = embedded dist)
	// ConfigPath is the user config file ("" = config.Path()); OnConfig is
	// called after the Config view saved it (e.g. to switch the diagnostic log).
	ConfigPath string
	OnConfig   func(config.Config)
}

// Server is the GUI backend.
type Server struct {
	opts    Options
	token   string
	ln      net.Listener
	srv     *http.Server
	mgr     *Manager
	hub     *hub
	static  http.Handler
	stopped chan struct{}
	once    sync.Once
	opened  chan struct{} // closed when a page first exchanges the session link
	tdl     toolsDL       // external tool downloads
	openOne sync.Once

	clients    atomic.Int64
	lastClient atomic.Int64 // unix nano of the last SSE disconnect or connect
	logged     atomic.Int64 // requests written to Logf (capped)
}

// maxLoggedRequests caps the request lines sent to Logf per run (diagnosis of
// the start, not an access log).
const maxLoggedRequests = 200

// statusWriter records the status code for the request log.
type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(c int) { w.code = c; w.ResponseWriter.WriteHeader(c) }

// Flush keeps server-sent events streaming through the wrapper.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// New starts listening (not serving yet) and loads the persisted jobs.
func New(o Options) (*Server, error) {
	if o.StateDir == "" {
		d, err := StateDir()
		if err != nil {
			return nil, err
		}
		o.StateDir = d
	}
	if err := os.MkdirAll(o.StateDir, 0o755); err != nil {
		return nil, err
	}
	var ln net.Listener
	var err error
	switch {
	case o.Port < 0:
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	default:
		port := o.Port
		if port == 0 {
			port = DefaultPort
		}
		ln, err = net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err != nil && o.Port == 0 {
			Logf("gui: port %d busy (%v): random port", port, err)
			ln, err = net.Listen("tcp", "127.0.0.1:0")
		}
	}
	if err != nil {
		Logf("gui: listen: %v", err)
		return nil, err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		ln.Close()
		return nil, err
	}
	st := &store{dir: o.StateDir}
	h := newHub()
	Logf("gui: state %s", o.StateDir)
	s := &Server{opts: o, token: hex.EncodeToString(b), ln: ln, hub: h, mgr: newManager(st, h), stopped: make(chan struct{}), opened: make(chan struct{})}
	s.mgr.cfgPath, s.mgr.version = o.ConfigPath, o.Version
	s.mgr.applyConfig()
	static := o.Static
	if static == nil {
		static = distFS()
	}
	s.static = http.FileServer(http.FS(static))
	s.lastClient.Store(time.Now().UnixNano())
	report.SetSink(s.mgr.onEvent)
	mux := http.NewServeMux()
	s.routes(mux)
	s.srv = &http.Server{Handler: s.guard(mux), ReadHeaderTimeout: 10 * time.Second}
	return s, nil
}

// Addr is host:port of the listener.
func (s *Server) Addr() string { return s.ln.Addr().String() }

// URL opens a session: the token in it is exchanged for a cookie.
func (s *Server) URL() string { return "http://" + s.Addr() + "/?t=" + s.token }

// Opened is closed once a page has loaded the session link: the window keeps
// retrying its first navigation until then.
func (s *Server) Opened() <-chan struct{} { return s.opened }

// Manager exposes the job manager (tests, shutdown).
func (s *Server) Manager() *Manager { return s.mgr }

// Serve blocks until Shutdown.
func (s *Server) Serve() error {
	go s.mgr.tick(s.stopped, 250*time.Millisecond)
	err := s.srv.Serve(s.ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown pauses and persists the active job, then stops the server.
func (s *Server) Shutdown() {
	s.once.Do(func() {
		s.mgr.PauseForExit()
		s.mgr.Wait()
		close(s.stopped)
		s.hub.close()
		s.srv.Close()
		report.SetSink(nil)
	})
}

// Idle reports whether no page is connected since d and no job runs (browser
// mode exits then: there is no window to close).
func (s *Server) Idle(d time.Duration) bool {
	return s.clients.Load() == 0 && s.mgr.Active() == "" && time.Since(time.Unix(0, s.lastClient.Load())) > d
}

// guard enforces the local-only, same-origin, token-bound access rules.
func (s *Server) guard(next http.Handler) http.Handler {
	inner := s.guardInner(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.logged.Load() >= maxLoggedRequests {
			inner.ServeHTTP(w, r)
			return
		}
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		if r.URL.Path == "/api/events" {
			Logf("http: %s %s (event stream opened)", r.Method, r.URL.Path)
		}
		t0 := time.Now()
		inner.ServeHTTP(sw, r)
		if s.logged.Add(1) <= maxLoggedRequests {
			Logf("http: %s %s -> %d (%s, %s)", r.Method, r.URL.Path, sw.code, time.Since(t0).Round(time.Millisecond), r.UserAgent())
		}
	})
}

func (s *Server) guardInner(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, port, _ := net.SplitHostPort(s.Addr())
		if h := r.Host; h != "127.0.0.1:"+port && h != "localhost:"+port {
			http.Error(w, "forbidden host", http.StatusForbidden) // DNS rebinding
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			o := r.Header.Get("Origin")
			if o != "" && o != "http://127.0.0.1:"+port && o != "http://localhost:"+port {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
			if sf := r.Header.Get("Sec-Fetch-Site"); sf != "" && sf != "same-origin" && sf != "none" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		if t := r.URL.Query().Get("t"); t != "" && r.URL.Path == "/" {
			if subtle.ConstantTimeCompare([]byte(t), []byte(s.token)) != 1 {
				http.Error(w, "invalid session link", http.StatusForbidden)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: cookieName, Value: s.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			s.openOne.Do(func() { close(s.opened) })
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		c, err := r.Cookie(cookieName)
		if err != nil || subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.token)) != 1 {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprintln(w, "onefold: open the link printed by `onefold gui` (this session is bound to it).")
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func writeJSONResp(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	switch {
	case errors.Is(err, errNotFound):
		code = http.StatusNotFound
	case errors.Is(err, errBusy), errors.Is(err, errBadStatus):
		code = http.StatusConflict
	}
	writeJSONResp(w, code, map[string]string{"error": err.Error()})
}

// itemQuery reads a list query: list, q, ext, dir, sort, offset, limit (max 5000).
func itemQuery(r *http.Request) ItemQuery {
	q := r.URL.Query()
	return ItemQuery{List: q.Get("list"), Q: q.Get("q"), Ext: q.Get("ext"), Dir: q.Get("dir"), Sort: q.Get("sort"),
		Offset: intParam(r, "offset", 0), Limit: min(intParam(r, "limit", 200), 5000)}
}

func intParam(r *http.Request, name string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(name)); err == nil && v >= 0 {
		return v
	}
	return def
}

func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/info", s.handleInfo)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/log", func(w http.ResponseWriter, r *http.Request) {
		if id := r.URL.Query().Get("job"); id != "" {
			es, total := s.mgr.st.readLog(id, r.URL.Query().Get("level"), intParam(r, "offset", 0), intParam(r, "limit", 500))
			writeJSONResp(w, 200, map[string]any{"entries": es, "total": total})
			return
		}
		writeJSONResp(w, 200, map[string]any{"entries": s.mgr.Log()})
	})
	mux.HandleFunc("/api/fs", s.handleFS)
	mux.HandleFunc("/api/naming", s.handleNaming)
	mux.HandleFunc("/api/naming/preview", s.handleNamingPreview)
	mux.HandleFunc("/api/cli", s.handleCLI)
	mux.HandleFunc("/api/wizard/scan", s.handleWizardScan)
	mux.HandleFunc("/api/wizard/preview", s.handleWizardPreview)
	mux.HandleFunc("/api/recipes", s.handleRecipes)
	mux.HandleFunc("/api/recipes/", s.handleRecipes)
	mux.HandleFunc("/api/journals", s.handleJournals)
	mux.HandleFunc("/api/golden", s.handleGolden)
	mux.HandleFunc("/api/jobs", s.handleJobs)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/tools", s.handleTools)
	mux.HandleFunc("/api/tools/", s.handleTools)
	mux.HandleFunc("/api/jobs/", s.handleJob)
	mux.HandleFunc("/api/mounts", s.handleMounts)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Cache-Control", "no-store")
		}
		s.static.ServeHTTP(w, r)
	})
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	tl := tools.Report()
	host, _ := os.Hostname()
	writeJSONResp(w, 200, map[string]any{
		"version": s.opts.Version, "build": s.opts.Build, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "cpus": runtime.NumCPU(),
		"lang": PreferredLang(r.Header.Get("Accept-Language")), "stateDir": s.opts.StateDir,
		"tools": tl, "toolsDisabled": tools.Disabled(), "active": s.mgr.Active(), "sep": string(filepath.Separator), "hostname": host,
	})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	ch := s.hub.subscribe()
	defer s.hub.unsubscribe(ch)
	s.clients.Add(1)
	s.lastClient.Store(time.Now().UnixNano())
	defer func() {
		s.clients.Add(-1)
		s.lastClient.Store(time.Now().UnixNano())
	}()
	fmt.Fprintf(w, "event: hello\ndata: {\"active\":%q}\n\n", s.mgr.Active())
	fl.Flush()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.stopped:
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case msg, ok := <-ch:
			if !ok {
				return
			}
			w.Write(msg)
			fl.Flush()
		}
	}
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSONResp(w, 200, map[string]any{"jobs": s.mgr.List(), "active": s.mgr.Active()})
	case http.MethodPost:
		var req struct {
			Kind   string          `json:"kind"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, err)
			return
		}
		if s.toolsBusy() {
			writeJSONResp(w, http.StatusConflict, map[string]string{"error": "a tool download is running: wait for it to finish"})
			return
		}
		j, err := s.mgr.Start(req.Kind, req.Params)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSONResp(w, 200, j)
	default:
		http.Error(w, "method", http.StatusMethodNotAllowed)
	}
}

// handleJob serves /api/jobs/{id}[/{action}].
func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	id, action, _ := strings.Cut(rest, "/")
	if r.Method == http.MethodGet {
		switch action {
		case "":
			j, ok := s.mgr.Get(id)
			if !ok {
				writeErr(w, errNotFound)
				return
			}
			writeJSONResp(w, 200, j)
		case "items":
			items, total, err := s.mgr.query(id, itemQuery(r))
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSONResp(w, 200, map[string]any{"items": items, "total": total})
		case "tree":
			j, _ := s.mgr.Get(id)
			var rooms []string
			if j != nil {
				rooms = summaryRooms(j.Summary)
			}
			nodes, more, err := s.mgr.tree(id, itemQuery(r), rooms)
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSONResp(w, 200, map[string]any{"nodes": nodes, "more": more})
		case "stats":
			s.handleStatsResult(w, id)
		case "exts":
			exts, err := s.mgr.exts(id, r.URL.Query().Get("list"))
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSONResp(w, 200, map[string]any{"exts": exts})
		default:
			http.NotFound(w, r)
		}
		return
	}
	if r.Method == http.MethodDelete && action == "" {
		if err := s.mgr.Delete(id); err != nil {
			writeErr(w, err)
			return
		}
		writeJSONResp(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	var err error
	switch action {
	case "execute":
		var req struct {
			Confirm int `json:"confirm"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		err = s.mgr.Execute(id, req.Confirm)
	case "continue":
		err = s.mgr.Continue(id)
	case "pause":
		err = s.mgr.Pause(id)
	case "resume":
		err = s.mgr.Resume(id)
	case "stop":
		err = s.mgr.Stop(id)
	case "exclude":
		var req ExcludeReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, err)
			return
		}
		j, err := s.mgr.Exclude(id, req)
		if errors.Is(err, errNeedsPreview) {
			writeJSONResp(w, http.StatusConflict, map[string]any{"error": err.Error(), "needsPreview": true})
			return
		}
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSONResp(w, 200, j)
		return
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	j, _ := s.mgr.Get(id)
	writeJSONResp(w, 200, j)
}
