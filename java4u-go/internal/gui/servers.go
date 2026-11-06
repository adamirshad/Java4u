package gui

// Modpack installs and dedicated servers for the app.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/adamirshad/Java4u/java4u-go/internal/chunky"
	"github.com/adamirshad/Java4u/java4u-go/internal/core"
)

// ---------------------------------------------------------------- modpacks

func (s *Server) handleInstallModpack(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Project string `json:"project"`
		Title   string `json:"title"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !chunkyAvailable() {
		writeErr(w, http.StatusBadRequest, chunky.ErrNotBuilt.Error())
		return
	}
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		writeErr(w, http.StatusConflict, "Wait for the current download or game to finish.")
		return
	}
	s.busy = true
	s.task = "modpack"
	s.progress = nil
	s.status = "Installing " + req.Title + "..."
	s.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 30*time.Minute)
		defer cancel()
		inst, err := core.InstallModpack(ctx, s.client, s.dirs, req.Project, "", s.setProgress)
		s.mu.Lock()
		s.busy = false
		s.task = ""
		s.progress = nil
		if err != nil {
			s.status = "Could not install " + req.Title + ": " + err.Error()
		} else {
			mods, _ := core.ListMods(inst.ModsDir(s.dirs))
			s.status = fmt.Sprintf("Installed %s as \"%s\" (Minecraft %s, %d mods). Press Launch Game to play.",
				inst.Modpack, inst.Name, inst.Version, len(mods))
			s.settings.LastInstance = inst.Name
		}
		settings := s.settings
		s.mu.Unlock()
		settings.Save(s.dirs)
	}()
	writeJSON(w, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------- servers

// serverProc is the dedicated server Java4U is running (at most one at a time).
type serverProc struct {
	mu       sync.Mutex
	name     string
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	starting bool
	running  bool
	log      []string
	logStart int
	epoch    int
}

func (p *serverProc) line(l string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.log = append(p.log, l)
	if extra := len(p.log) - maxLogLines; extra > 0 {
		p.log = append([]string(nil), p.log[extra:]...)
		p.logStart += extra
	}
}

func lanIP() string {
	conn, err := net.Dial("udp", "192.0.2.1:9") // no packets are sent; this just picks the outgoing interface
	if err != nil {
		return ""
	}
	defer conn.Close()
	if a, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return a.IP.String()
	}
	return ""
}

func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	type view struct {
		*core.Server
		EULA bool `json:"eula"`
		Mods int  `json:"mods"`
	}
	out := []view{}
	for _, sv := range core.ListServers(s.dirs) {
		mods, _ := core.ListMods(sv.ModsDir(s.dirs))
		out = append(out, view{sv, core.EULAAccepted(sv.Dir(s.dirs)), len(mods)})
	}
	p := &s.srv
	p.mu.Lock()
	running, starting, name := p.running, p.starting, p.name
	p.mu.Unlock()
	writeJSON(w, map[string]any{
		"servers": out, "running": running, "starting": starting, "active": name, "lan_ip": lanIP(),
	})
}

func (s *Server) handleCreateServer(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name, Version string }
	if !readJSON(w, r, &req) {
		return
	}
	sv, err := core.CreateServer(s.dirs, strings.TrimSpace(req.Name), req.Version)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "Could not create the server: "+err.Error())
		return
	}
	writeJSON(w, sv)
}

func (s *Server) handleSaveServer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		MemoryMB int    `json:"memory_mb"`
		Loader   string `json:"loader"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	sv, err := core.LoadServer(s.dirs, req.Name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.MemoryMB < 512 {
		writeErr(w, http.StatusBadRequest, "memory must be at least 512 MB")
		return
	}
	sv.MemoryMB = req.MemoryMB
	switch req.Loader {
	case "", "vanilla":
		sv.Loader = ""
	case core.LoaderChunky:
		if !core.ChunkySupports(sv.Version) {
			writeErr(w, http.StatusBadRequest, "Chunky supports Minecraft 26.1 and newer.")
			return
		}
		sv.Loader = core.LoaderChunky
	default:
		writeErr(w, http.StatusBadRequest, "unknown mod loader")
		return
	}
	if err := sv.Save(s.dirs); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, sv)
}

func (s *Server) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name string }
	if !readJSON(w, r, &req) {
		return
	}
	p := &s.srv
	p.mu.Lock()
	busy := (p.running || p.starting) && p.name == req.Name
	p.mu.Unlock()
	if busy {
		writeErr(w, http.StatusConflict, "Stop the server before deleting it.")
		return
	}
	if err := core.DeleteServer(s.dirs, req.Name); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleAcceptEULA records the user's own agreement, given by ticking the box in the app.
func (s *Server) handleAcceptEULA(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Agree bool   `json:"agree"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	sv, err := core.LoadServer(s.dirs, req.Name)
	if err != nil || !req.Agree {
		writeErr(w, http.StatusBadRequest, "nothing to record")
		return
	}
	if err := core.AcceptEULA(sv.Dir(s.dirs)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleStartServer(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name string }
	if !readJSON(w, r, &req) {
		return
	}
	sv, err := core.LoadServer(s.dirs, req.Name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !core.EULAAccepted(sv.Dir(s.dirs)) {
		writeErr(w, http.StatusBadRequest, "Tick the box to agree to Minecraft's EULA first.")
		return
	}
	p := &s.srv
	p.mu.Lock()
	if p.running || p.starting {
		p.mu.Unlock()
		writeErr(w, http.StatusConflict, "A server is already running. Stop it first.")
		return
	}
	p.name, p.starting, p.log, p.logStart = sv.Name, true, nil, 0
	p.epoch++
	p.mu.Unlock()

	go s.runServer(sv)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) runServer(sv *core.Server) {
	p := &s.srv
	done := func(msg string) {
		p.line(msg)
		p.mu.Lock()
		p.starting, p.running, p.cmd, p.stdin = false, false, nil, nil
		p.mu.Unlock()
	}
	progress := func(stage string, d, t int) {
		if d == t {
			p.line(fmt.Sprintf("[Java4U] %s ready (%d files)", stage, t))
		}
	}
	p.line("[Java4U] Preparing Minecraft " + sv.Version + " server...")
	v, files, err := core.InstallServer(s.ctx, s.client, s.dirs, sv.Version, progress)
	if err != nil {
		done("[Java4U] Error: " + err.Error())
		return
	}
	java, err := core.ResolveJava(s.ctx, s.client, s.dirs, v, sv.JavaPath, progress)
	if err != nil {
		done("[Java4U] Error: " + err.Error())
		return
	}
	var ch *core.ChunkyLaunch
	if sv.Loader == core.LoaderChunky {
		if ch, err = chunky.Prepare(s.ctx, s.client, s.dirs, progress); err != nil {
			done("[Java4U] Error: " + err.Error())
			return
		}
	}
	cmd, err := core.BuildServerCommand(s.dirs, v, sv, files, java, ch)
	if err != nil {
		done("[Java4U] Error: " + err.Error())
		return
	}
	core.HideConsole(cmd)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		done("[Java4U] Error: could not start Java: " + err.Error())
		return
	}
	p.mu.Lock()
	p.cmd, p.stdin, p.starting, p.running = cmd, stdin, false, true
	p.mu.Unlock()

	br := bufio.NewReader(stdout)
	for {
		line, err := br.ReadString('\n')
		if line = strings.TrimRight(line, "\r\n"); line != "" {
			p.line(line)
		}
		if err != nil {
			break
		}
	}
	if err := cmd.Wait(); err != nil {
		done("[Java4U] The server stopped with " + err.Error())
		return
	}
	done("[Java4U] The server stopped.")
}

func (s *Server) handleServerCommand(w http.ResponseWriter, r *http.Request) {
	var req struct{ Command string }
	if !readJSON(w, r, &req) {
		return
	}
	cmd := strings.TrimSpace(strings.TrimPrefix(req.Command, "/"))
	if cmd == "" || strings.ContainsAny(cmd, "\r\n") {
		writeErr(w, http.StatusBadRequest, "enter one command")
		return
	}
	p := &s.srv
	p.mu.Lock()
	stdin := p.stdin
	p.mu.Unlock()
	if stdin == nil {
		writeErr(w, http.StatusConflict, "The server isn't running.")
		return
	}
	p.line("> " + cmd)
	if _, err := io.WriteString(stdin, cmd+"\n"); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleStopServer(w http.ResponseWriter, r *http.Request) {
	p := &s.srv
	p.mu.Lock()
	stdin, cmd := p.stdin, p.cmd
	p.mu.Unlock()
	if stdin == nil || cmd == nil {
		writeErr(w, http.StatusConflict, "The server isn't running.")
		return
	}
	io.WriteString(stdin, "stop\n")
	// If it hasn't stopped after a while, end it.
	go func() {
		time.Sleep(45 * time.Second)
		p.mu.Lock()
		still := p.cmd == cmd
		p.mu.Unlock()
		if still && cmd.Process != nil {
			cmd.Process.Kill()
		}
	}()
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleServerConsole(w http.ResponseWriter, r *http.Request) {
	var epoch, since int
	fmt.Sscan(r.URL.Query().Get("epoch"), &epoch)
	fmt.Sscan(r.URL.Query().Get("since"), &since)
	p := &s.srv
	p.mu.Lock()
	defer p.mu.Unlock()
	reset := epoch != p.epoch
	from := 0
	if !reset {
		from = min(max(since, p.logStart)-p.logStart, len(p.log))
	}
	writeJSON(w, map[string]any{
		"name": p.name, "running": p.running, "starting": p.starting,
		"epoch": p.epoch, "reset": reset, "log": append([]string{}, p.log[from:]...), "next": p.logStart + len(p.log),
	})
}

// ---------------------------------------------------------------- server mods

func (s *Server) handleServerMods(w http.ResponseWriter, r *http.Request) {
	sv, err := core.LoadServer(s.dirs, r.URL.Query().Get("server"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	mods, err := core.ListMods(sv.ModsDir(s.dirs))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"server": sv.Name, "loader": sv.Loader, "mods": mods})
}

func (s *Server) handleServerModInstall(w http.ResponseWriter, r *http.Request) {
	var req struct{ Server, Project string }
	if !readJSON(w, r, &req) {
		return
	}
	sv, err := core.LoadServer(s.dirs, req.Server)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if sv.Loader != core.LoaderChunky {
		if !core.ChunkySupports(sv.Version) {
			writeErr(w, http.StatusBadRequest, "Server mods run on Chunky, which supports Minecraft 26.1 and newer.")
			return
		}
		sv.Loader = core.LoaderChunky
		if err := sv.Save(s.dirs); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	defer cancel()
	files, err := core.InstallMod(ctx, s.client, sv.ModsDir(s.dirs), req.Project, sv.Version, core.SideServer)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	names := []string{}
	for _, f := range files {
		names = append(names, strings.TrimSuffix(f.File, ".jar"))
	}
	msg := "Already installed."
	if len(names) > 0 {
		msg = "Installed " + strings.Join(names, ", ") + " on " + sv.Name + ". Restart the server to load it."
	}
	writeJSON(w, map[string]any{"message": msg})
}

func (s *Server) handleServerModChange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Server  string `json:"server"`
		File    string `json:"file"`
		Enabled bool   `json:"enabled"`
		Remove  bool   `json:"remove"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	sv, err := core.LoadServer(s.dirs, req.Server)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Remove {
		err = core.RemoveMod(sv.ModsDir(s.dirs), req.File)
	} else {
		err = core.SetModEnabled(sv.ModsDir(s.dirs), req.File, req.Enabled)
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
