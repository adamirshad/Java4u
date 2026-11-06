// Package gui serves Java4U's desktop interface: a small web app bound to 127.0.0.1,
// shown in an app window (or a browser tab). All game work happens in Go.
package gui

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/adamirshad/Java4u/java4u-go/internal/chunky"
	"github.com/adamirshad/Java4u/java4u-go/internal/core"
)

//go:embed web
var webFS embed.FS

const (
	maxLogLines = 3000
	cookieName  = "java4u_session"
	// idleExit closes Java4U once the window has been gone this long and nothing is running.
	// Hidden browser tabs may only poll about once a minute, so this stays generous.
	idleExit = 3 * time.Minute
)

type progress struct {
	Stage string `json:"stage"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

type deviceCode struct {
	URI  string `json:"uri"`
	Code string `json:"code"`
}

// Server holds all state shared between HTTP handlers and background tasks.
type Server struct {
	dirs   core.Dirs
	client *http.Client
	token  string
	host   string // "127.0.0.1:port"
	ctx    context.Context
	cancel context.CancelFunc

	mu          sync.Mutex
	log         []string
	logStart    int // sequence number of log[0]
	logEpoch    int // bumps when the log is cleared
	status      string
	progress    *progress
	busy        bool
	running     bool
	loggingIn   bool
	deviceCode  *deviceCode
	versions    []core.VersionEntry
	versionsErr string
	settings    core.Settings
	lastSeen    time.Time

	cache contentCache
	// task says what busy is doing: "play", "modpack" or "update".
	task string
	srv  serverProc
	upd  updateState
}

// Start listens on addr (normally "127.0.0.1:0") and returns the server and the URL
// to open, which carries a one-time login token.
func Start(dirs core.Dirs, addr string) (*Server, string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", err
	}
	tok := make([]byte, 24)
	if _, err := rand.Read(tok); err != nil {
		return nil, "", err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		dirs:     dirs,
		client:   core.NewClient(),
		token:    hex.EncodeToString(tok),
		host:     ln.Addr().String(),
		ctx:      ctx,
		cancel:   cancel,
		settings: core.LoadSettings(dirs),
		lastSeen: time.Now(),
	}

	go s.fetchVersions()
	go s.watchIdle()
	go s.autoCheckUpdates()

	srv := &http.Server{Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server: %v", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdown, done := context.WithTimeout(context.Background(), 2*time.Second)
		defer done()
		srv.Shutdown(shutdown)
	}()

	return s, fmt.Sprintf("http://%s/?t=%s", s.host, s.token), nil
}

// Done is closed when the user quits or the window has been gone for a while.
func (s *Server) Done() <-chan struct{} { return s.ctx.Done() }

func (s *Server) fetchVersions() {
	m, err := core.FetchManifest(s.ctx, s.client)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.versionsErr = err.Error()
		s.status = "Could not load the version list: " + err.Error()
		return
	}
	s.versions = m.Versions
}

func (s *Server) watchIdle() {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			s.mu.Lock()
			s.srv.mu.Lock()
			serving := s.srv.running || s.srv.starting
			s.srv.mu.Unlock()
			idle := !s.busy && !s.loggingIn && !serving && time.Since(s.lastSeen) > idleExit
			s.mu.Unlock()
			if idle {
				log.Printf("window closed; exiting")
				s.cancel()
				return
			}
		}
	}
}

// ------------------------------------------------------------ shared state helpers

func (s *Server) logLine(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = append(s.log, line)
	if extra := len(s.log) - maxLogLines; extra > 0 {
		s.log = append([]string(nil), s.log[extra:]...)
		s.logStart += extra
	}
}

func (s *Server) setStatus(msg string) {
	s.mu.Lock()
	s.status = msg
	s.mu.Unlock()
}

func (s *Server) setProgress(stage string, done, total int) {
	s.mu.Lock()
	s.progress = &progress{stage, done, total}
	s.mu.Unlock()
}

// ------------------------------------------------------------ routing and security

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFS, "web")
	files := http.FileServer(http.FS(static))

	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.Handle("GET /app.js", files)
	mux.Handle("GET /servers.js", files)
	mux.Handle("GET /installed.js", files)
	mux.Handle("GET /style.css", files)
	mux.Handle("GET /goose.svg", files)
	mux.Handle("GET /hero.svg", files)

	mux.HandleFunc("GET /api/state", s.auth(s.handleState))
	mux.HandleFunc("GET /api/data", s.auth(s.handleData))
	mux.HandleFunc("POST /api/instances", s.auth(s.handleCreateInstance))
	mux.HandleFunc("POST /api/instances/save", s.auth(s.handleSaveInstance))
	mux.HandleFunc("POST /api/instances/delete", s.auth(s.handleDeleteInstance))
	mux.HandleFunc("POST /api/play", s.auth(s.handlePlay))
	mux.HandleFunc("POST /api/login", s.auth(s.handleLogin))
	mux.HandleFunc("POST /api/accounts/remove", s.auth(s.handleRemoveAccount))
	mux.HandleFunc("POST /api/settings", s.auth(s.handleSettings))
	mux.HandleFunc("POST /api/wallpapers", s.auth(s.handleUploadWallpaper))
	mux.HandleFunc("POST /api/wallpapers/delete", s.auth(s.handleDeleteWallpaper))
	mux.HandleFunc("GET /wallpapers/{name}", s.auth(s.handleWallpaperFile))
	mux.HandleFunc("POST /api/quit", s.auth(s.handleQuit))
	mux.HandleFunc("POST /api/update/check", s.auth(s.handleCheckUpdate))
	mux.HandleFunc("POST /api/update/apply", s.auth(s.handleApplyUpdate))
	mux.HandleFunc("POST /api/update/restart", s.auth(s.handleRestart))
	mux.HandleFunc("GET /api/news", s.auth(s.handleNews))
	mux.HandleFunc("GET /api/browse", s.auth(s.handleBrowse))
	mux.HandleFunc("GET /api/avatar/{uuid}", s.auth(s.handleAvatar))
	mux.HandleFunc("GET /api/skin/{uuid}", s.auth(s.handleSkin))
	mux.Handle("GET /skin3d.js", files)
	mux.Handle("GET /gamefiles.js", files)
	mux.HandleFunc("GET /api/mods", s.auth(s.handleListMods))
	mux.HandleFunc("POST /api/mods/install", s.auth(s.handleInstallMod))
	mux.HandleFunc("POST /api/mods/toggle", s.auth(s.handleToggleMod))
	mux.HandleFunc("POST /api/mods/remove", s.auth(s.handleRemoveMod))
	mux.HandleFunc("GET /api/mods/installed", s.auth(s.handleInstalled))
	mux.HandleFunc("POST /api/mods/toggle-all", s.auth(s.handleToggleAll))
	mux.HandleFunc("POST /api/modpacks/install", s.auth(s.handleInstallModpack))
	mux.HandleFunc("POST /api/modpacks/import", s.auth(s.handleImportMrpack))
	mux.HandleFunc("GET /api/instances/{name}/export", s.auth(s.handleExportMrpack))
	mux.HandleFunc("GET /api/instances/{name}/files", s.auth(s.handleGameFiles))
	mux.HandleFunc("GET /api/instances/{name}/screenshots/{file}", s.auth(s.handleScreenshot))
	mux.HandleFunc("GET /api/instances/{name}/worlds/{folder}/icon", s.auth(s.handleWorldIcon))
	mux.HandleFunc("POST /api/open-folder", s.auth(s.handleOpenFolder))
	mux.HandleFunc("GET /api/servers", s.auth(s.handleListServers))
	mux.HandleFunc("POST /api/servers", s.auth(s.handleCreateServer))
	mux.HandleFunc("POST /api/servers/save", s.auth(s.handleSaveServer))
	mux.HandleFunc("POST /api/servers/delete", s.auth(s.handleDeleteServer))
	mux.HandleFunc("POST /api/servers/eula", s.auth(s.handleAcceptEULA))
	mux.HandleFunc("POST /api/servers/start", s.auth(s.handleStartServer))
	mux.HandleFunc("POST /api/servers/stop", s.auth(s.handleStopServer))
	mux.HandleFunc("POST /api/servers/command", s.auth(s.handleServerCommand))
	mux.HandleFunc("GET /api/servers/console", s.auth(s.handleServerConsole))
	mux.HandleFunc("GET /api/servers/mods", s.auth(s.handleServerMods))
	mux.HandleFunc("POST /api/servers/mods/install", s.auth(s.handleServerModInstall))
	mux.HandleFunc("POST /api/servers/mods/change", s.auth(s.handleServerModChange))

	return s.checkHost(mux)
}

// checkHost rejects requests whose Host header is not our own address, which blocks
// DNS-rebinding attacks from web pages.
func (s *Server) checkHost(next http.Handler) http.Handler {
	_, port, _ := net.SplitHostPort(s.host)
	allowed := map[string]bool{s.host: true, "localhost:" + port: true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed[r.Host] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) validCookie(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	return err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.token)) == 1
}

func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.validCookie(r) {
			writeErr(w, http.StatusUnauthorized, "not signed in to this Java4U window")
			return
		}
		if r.Method == http.MethodPost {
			if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
				writeErr(w, http.StatusForbidden, "cross-origin request refused")
				return
			}
		}
		s.mu.Lock()
		s.lastSeen = time.Now()
		s.mu.Unlock()
		h(w, r)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if t := r.URL.Query().Get("t"); t != "" {
		if subtle.ConstantTimeCompare([]byte(t), []byte(s.token)) != 1 {
			http.Error(w, "This link has expired. Start Java4U again.", http.StatusForbidden)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name: cookieName, Value: s.token, Path: "/",
			HttpOnly: true, SameSite: http.SameSiteStrictMode,
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if !s.validCookie(r) {
		http.Error(w, "This window is not connected to Java4U. Start Java4U again.", http.StatusForbidden)
		return
	}
	page, _ := webFS.ReadFile("web/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: https://cdn.modrinth.com https://launchercontent.mojang.com; style-src 'self'; script-src 'self'; frame-ancestors 'none'")
	w.Write(page)
}

// ------------------------------------------------------------ JSON helpers

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeErr(w, http.StatusUnsupportedMediaType, "expected JSON")
		return false
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request: "+err.Error())
		return false
	}
	return true
}

// ------------------------------------------------------------ read endpoints

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	var epoch, since int
	fmt.Sscan(r.URL.Query().Get("epoch"), &epoch)
	fmt.Sscan(r.URL.Query().Get("since"), &since)

	s.mu.Lock()
	defer s.mu.Unlock()
	reset := epoch != s.logEpoch
	from := 0
	if !reset {
		from = max(since, s.logStart) - s.logStart
		from = min(from, len(s.log))
	}
	writeJSON(w, map[string]any{
		"busy":        s.busy,
		"task":        s.task,
		"running":     s.running,
		"logging_in":  s.loggingIn,
		"status":      s.status,
		"progress":    s.progress,
		"device_code": s.deviceCode,
		"log_epoch":   s.logEpoch,
		"log_reset":   reset,
		"log":         append([]string{}, s.log[from:]...),
		"log_next":    s.logStart + len(s.log),
	})
}

type accountView struct {
	Username string `json:"username"`
	UUID     string `json:"uuid"`
}

type profileView struct {
	ID          core.Profile `json:"id"`
	Label       string       `json:"label"`
	Description string       `json:"description"`
}

func (s *Server) handleData(w http.ResponseWriter, r *http.Request) {
	instances, _ := core.ListInstances(s.dirs)
	store, err := core.LoadAccounts(s.dirs)
	if err != nil {
		store = &core.AccountStore{}
	}
	accounts := []accountView{}
	for _, a := range store.Accounts {
		accounts = append(accounts, accountView{a.Username, a.UUID})
	}
	profiles := []profileView{}
	for _, p := range core.AllProfiles {
		profiles = append(profiles, profileView{p, p.Label(), p.Description()})
	}

	s.mu.Lock()
	type versionView struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
	}
	versions := make([]versionView, 0, len(s.versions))
	for _, v := range s.versions {
		versions = append(versions, versionView{v.ID, v.Kind})
	}
	settings := s.settings
	versionsErr := s.versionsErr
	s.mu.Unlock()

	writeJSON(w, map[string]any{
		"app_version":    core.Version,
		"data_dir":       s.dirs.Root,
		"instances":      instances,
		"accounts":       accounts,
		"has_client_id":  store.ClientID != "",
		"token_store":    core.KeychainName(),
		"settings":       settings,
		"versions":       versions,
		"versions_error": versionsErr,
		"memory": map[string]int{
			"max":         core.MaxMemoryMB(),
			"recommended": core.RecommendedMemoryMB(),
		},
		"profiles":   profiles,
		"chunky":     map[string]any{"version": chunky.Version, "available": chunkyAvailable()},
		"wallpapers": core.ListWallpapers(s.dirs),
		"update":     s.updateInfo(),
	})
}

// ------------------------------------------------------------ instances

func (s *Server) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name, Version string }
	if !readJSON(w, r, &req) {
		return
	}
	inst, err := core.CreateInstance(s.dirs, strings.TrimSpace(req.Name), req.Version)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "Could not create instance: "+err.Error())
		return
	}
	s.setStatus(fmt.Sprintf("Created '%s'.", inst.Name))
	writeJSON(w, inst)
}

func (s *Server) handleSaveInstance(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		MemoryMB int    `json:"memory_mb"`
		Profile  string `json:"profile"`
		JVMArgs  string `json:"jvm_args"`
		JavaPath string `json:"java_path"`
		Loader   string `json:"loader"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	inst, err := core.LoadInstance(s.dirs, req.Name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	switch req.Loader {
	case "", "vanilla":
		inst.Loader = ""
	case core.LoaderChunky:
		if !chunky.SupportsVersion(inst.Version) {
			writeErr(w, http.StatusBadRequest, "Chunky supports Minecraft 26.1 and newer.")
			return
		}
		inst.Loader = core.LoaderChunky
	default:
		writeErr(w, http.StatusBadRequest, "unknown mod loader")
		return
	}
	if req.MemoryMB < 512 {
		writeErr(w, http.StatusBadRequest, "memory must be at least 512 MB")
		return
	}
	p, err := core.ParseProfile(req.Profile)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	inst.MemoryMB = req.MemoryMB
	inst.Profile = p
	inst.JVMArgs = strings.Fields(req.JVMArgs)
	inst.JavaPath = strings.TrimSpace(req.JavaPath)
	if err := inst.Save(s.dirs); err != nil {
		writeErr(w, http.StatusInternalServerError, "Could not save: "+err.Error())
		return
	}
	s.setStatus("Saved.")
	writeJSON(w, inst)
}

func (s *Server) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name string }
	if !readJSON(w, r, &req) {
		return
	}
	s.mu.Lock()
	busy := s.busy
	s.mu.Unlock()
	if busy {
		writeErr(w, http.StatusConflict, "Wait until the game has closed before deleting an instance.")
		return
	}
	if err := core.DeleteInstance(s.dirs, req.Name); err != nil {
		writeErr(w, http.StatusInternalServerError, "Could not delete: "+err.Error())
		return
	}
	s.setStatus(fmt.Sprintf("Deleted '%s'.", req.Name))
	writeJSON(w, map[string]bool{"ok": true})
}

// ------------------------------------------------------------ play

func (s *Server) handlePlay(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Instance        string `json:"instance"`
		Account         string `json:"account"`
		Offline         bool   `json:"offline"`
		OfflineUsername string `json:"offline_username"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	inst, err := core.LoadInstance(s.dirs, req.Instance)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Offline && !core.ValidUsername(req.OfflineUsername) {
		writeErr(w, http.StatusBadRequest, "Offline username must be 3-16 characters: letters, digits or underscore.")
		return
	}

	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		writeErr(w, http.StatusConflict, "Already running.")
		return
	}
	s.busy = true
	s.task = "play"
	s.progress = nil
	s.log = nil
	s.logStart = 0
	s.logEpoch++
	s.settings.LastInstance = inst.Name
	if req.Offline {
		s.settings.OfflineUsername = req.OfflineUsername
	}
	settings := s.settings
	s.mu.Unlock()
	settings.Save(s.dirs)

	offline := ""
	if req.Offline {
		offline = req.OfflineUsername
	}
	go func() {
		err := s.play(inst, offline, req.Account)
		s.mu.Lock()
		s.busy = false
		s.task = ""
		s.running = false
		s.progress = nil
		s.mu.Unlock()
		if err != nil {
			s.logLine("Error: " + err.Error())
		} else {
			s.logLine("Game closed.")
		}
	}()
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) play(inst *core.Instance, offline, account string) error {
	ctx := s.ctx
	// Sort out the account first, so a missing login fails before any big download.
	var acct core.Account
	var err error
	if offline != "" {
		acct, err = core.OfflineAccount(offline)
	} else {
		acct, err = core.AccountForLaunch(ctx, s.client, s.dirs, account, "")
	}
	if err != nil {
		return err
	}

	v, err := core.InstallVersion(ctx, s.client, s.dirs, inst.Version, false, s.setProgress)
	if err != nil {
		return err
	}
	java, err := core.ResolveJava(ctx, s.client, s.dirs, v, inst.JavaPath, s.setProgress)
	if err != nil {
		return err
	}
	s.logLine("Using Java: " + java)

	var ch *core.ChunkyLaunch
	if inst.Loader == core.LoaderChunky {
		ch, err = chunky.Prepare(ctx, s.client, s.dirs, s.setProgress)
		if err != nil {
			return err
		}
		s.logLine("Starting with Chunky " + chunky.Version)
	}

	cmd, err := core.BuildCommand(s.dirs, v, core.LaunchOptions{
		Java:         java,
		GameDir:      inst.GameDir(s.dirs),
		Account:      acct,
		MemoryMB:     inst.MemoryMB,
		ExtraJVMArgs: inst.EffectiveJVMArgs(),
		Chunky:       ch,
	})
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.progress = nil
	s.running = true
	s.mu.Unlock()
	s.logLine("Launching Minecraft...")
	return s.runLogged(cmd)
}

// runLogged runs the game, streaming its output into the log until it exits.
// The game keeps running even if Java4U's window is closed.
func (s *Server) runLogged(cmd *exec.Cmd) error {
	core.HideConsole(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Java: %w", err)
	}

	var wg sync.WaitGroup
	for _, pipe := range []io.Reader{stdout, stderr} {
		wg.Add(1)
		go func(r io.Reader) {
			defer wg.Done()
			br := bufio.NewReader(r)
			for {
				line, err := br.ReadString('\n')
				if line = strings.TrimRight(line, "\r\n"); line != "" {
					s.logLine(line)
				}
				if err != nil {
					return
				}
			}
		}(pipe)
	}
	wg.Wait()
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("Minecraft exited with %w", err)
	}
	return nil
}

// ------------------------------------------------------------ accounts

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ClientID string `json:"client_id"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	store, _ := core.LoadAccounts(s.dirs)
	id := core.ClientIDFor(strings.TrimSpace(req.ClientID), store)

	s.mu.Lock()
	if s.loggingIn {
		s.mu.Unlock()
		writeErr(w, http.StatusConflict, "Sign-in is already in progress.")
		return
	}
	s.loggingIn = true
	s.status = "Requesting a sign-in code..."
	s.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 20*time.Minute)
		defer cancel()
		acct, err := core.Login(ctx, s.client, s.dirs, id, func(dc *core.DeviceCode) {
			s.mu.Lock()
			s.deviceCode = &deviceCode{URI: dc.VerificationURI, Code: dc.UserCode}
			s.status = "Waiting for you to finish signing in..."
			s.mu.Unlock()
		})
		s.mu.Lock()
		s.loggingIn = false
		s.deviceCode = nil
		if err != nil {
			s.status = "Sign-in failed: " + err.Error()
		} else {
			s.status = fmt.Sprintf("Signed in as %s.", acct.Username)
		}
		s.mu.Unlock()
	}()
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleRemoveAccount(w http.ResponseWriter, r *http.Request) {
	var req struct{ Username string }
	if !readJSON(w, r, &req) {
		return
	}
	store, err := core.LoadAccounts(s.dirs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if store.Remove(req.Username) {
		if err := store.Save(s.dirs); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ------------------------------------------------------------ settings and wallpapers

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ShowSnapshots   *bool    `json:"show_snapshots"`
		WallpaperDim    *float64 `json:"wallpaper_dim"`
		Wallpaper       *string  `json:"wallpaper"`
		OfflineUsername *string  `json:"offline_username"`
		PlayOffline     *bool    `json:"play_offline"`
		LastAccount     *string  `json:"last_account"`
		LastInstance    *string  `json:"last_instance"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if req.Wallpaper != nil && *req.Wallpaper != "" {
		if _, err := core.WallpaperPath(s.dirs, *req.Wallpaper); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	s.mu.Lock()
	if req.ShowSnapshots != nil {
		s.settings.ShowSnapshots = *req.ShowSnapshots
	}
	if req.WallpaperDim != nil {
		s.settings.WallpaperDim = min(max(*req.WallpaperDim, 0), 0.9)
	}
	if req.Wallpaper != nil {
		s.settings.Wallpaper = *req.Wallpaper
	}
	if req.OfflineUsername != nil {
		s.settings.OfflineUsername = *req.OfflineUsername
	}
	if req.PlayOffline != nil {
		s.settings.PlayOffline = *req.PlayOffline
	}
	if req.LastAccount != nil {
		s.settings.LastAccount = *req.LastAccount
	}
	if req.LastInstance != nil {
		s.settings.LastInstance = *req.LastInstance
	}
	settings := s.settings
	s.mu.Unlock()
	if err := settings.Save(s.dirs); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, settings)
}

func (s *Server) handleUploadWallpaper(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 65<<20)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "Could not read that file: "+err.Error())
		return
	}
	defer file.Close()
	name, err := core.ImportWallpaper(s.dirs, header.Filename, file)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "Could not import that image: "+err.Error())
		return
	}
	s.mu.Lock()
	s.settings.Wallpaper = name
	s.status = "Wallpaper added."
	settings := s.settings
	s.mu.Unlock()
	settings.Save(s.dirs)
	writeJSON(w, map[string]string{"name": name})
}

func (s *Server) handleDeleteWallpaper(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name string }
	if !readJSON(w, r, &req) {
		return
	}
	if err := core.RemoveWallpaper(s.dirs, req.Name); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.Lock()
	if s.settings.Wallpaper == req.Name {
		s.settings.Wallpaper = ""
	}
	settings := s.settings
	s.mu.Unlock()
	settings.Save(s.dirs)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleWallpaperFile(w http.ResponseWriter, r *http.Request) {
	p, err := core.WallpaperPath(s.dirs, r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, p)
}

func (s *Server) handleQuit(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]bool{"ok": true})
	go func() {
		time.Sleep(200 * time.Millisecond)
		s.cancel()
	}()
}
