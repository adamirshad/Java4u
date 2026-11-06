package gui

// Checking for and installing Java4U updates from GitHub Releases.

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/adamirshad/Java4u/java4u-go/internal/core"
)

type updateState struct {
	mu        sync.Mutex
	available *core.Release // newer than this build, or nil
	installed bool          // downloaded and swapped in; needs a restart
	restart   bool          // the user asked to restart into the new version
}

type updateView struct {
	Version   string `json:"version"`
	Page      string `json:"page"`
	Installed bool   `json:"installed"`
}

func (s *Server) updateInfo() *updateView {
	s.upd.mu.Lock()
	defer s.upd.mu.Unlock()
	if s.upd.available == nil {
		return nil
	}
	return &updateView{s.upd.available.Version(), s.upd.available.Page, s.upd.installed}
}

// autoCheckUpdates runs once at start-up. Development builds and JAVA4U_NO_UPDATE_CHECK
// skip it; "Check for updates" on the About page always works.
func (s *Server) autoCheckUpdates() {
	if strings.Contains(core.Version, "-dev") || os.Getenv("JAVA4U_NO_UPDATE_CHECK") != "" {
		return
	}
	select {
	case <-time.After(3 * time.Second): // let the window load first
	case <-s.ctx.Done():
		return
	}
	s.checkUpdate()
}

func (s *Server) checkUpdate() (*core.Release, error) {
	ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
	defer cancel()
	rel, err := core.LatestRelease(ctx, s.client, core.ReleasesURL)
	if err != nil {
		return nil, err
	}
	if rel.Draft || rel.Pre || !core.NewerVersion(rel.Version(), core.Version) {
		rel = nil
	}
	s.upd.mu.Lock()
	if !s.upd.installed {
		s.upd.available = rel
	}
	s.upd.mu.Unlock()
	return rel, nil
}

func (s *Server) handleCheckUpdate(w http.ResponseWriter, r *http.Request) {
	rel, err := s.checkUpdate()
	if errors.Is(err, core.ErrNoReleases) {
		writeJSON(w, map[string]any{"update": nil, "message": "No Java4U releases have been published yet."})
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, "Could not check for updates: "+err.Error())
		return
	}
	if rel == nil {
		writeJSON(w, map[string]any{"update": nil, "message": "You have the newest Java4U (v" + core.Version + ")."})
		return
	}
	writeJSON(w, map[string]any{"update": s.updateInfo()})
}

func (s *Server) handleApplyUpdate(w http.ResponseWriter, r *http.Request) {
	s.upd.mu.Lock()
	rel, installed := s.upd.available, s.upd.installed
	s.upd.mu.Unlock()
	if rel == nil {
		writeErr(w, http.StatusBadRequest, "There is no update to install.")
		return
	}
	if installed {
		writeErr(w, http.StatusBadRequest, "The update is already installed. Restart Java4U to use it.")
		return
	}
	s.mu.Lock()
	if s.busy || s.running {
		s.mu.Unlock()
		writeErr(w, http.StatusConflict, "Wait for the current download or game to finish.")
		return
	}
	s.busy = true
	s.task = "update"
	s.progress = nil
	s.status = "Downloading Java4U " + rel.Version() + "..."
	s.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 15*time.Minute)
		defer cancel()
		err := core.ApplyUpdate(ctx, s.client, rel, "", s.setProgress)
		if err == nil {
			s.upd.mu.Lock()
			s.upd.installed = true
			s.upd.mu.Unlock()
		}
		s.mu.Lock()
		s.busy = false
		s.task = ""
		s.progress = nil
		if err != nil {
			s.status = "Could not update: " + err.Error()
		} else {
			s.status = "Java4U " + rel.Version() + " is installed. Restart Java4U to start using it."
		}
		s.mu.Unlock()
	}()
	writeJSON(w, map[string]bool{"ok": true})
}

// handleRestart closes this Java4U; main starts the new version once the port is free.
func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	s.upd.mu.Lock()
	ok := s.upd.installed
	s.upd.restart = ok
	s.upd.mu.Unlock()
	if !ok {
		writeErr(w, http.StatusBadRequest, "No update is waiting.")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
	go func() {
		time.Sleep(200 * time.Millisecond)
		s.cancel()
	}()
}

// RestartRequested reports whether Java4U should start again after it closes.
func (s *Server) RestartRequested() bool {
	s.upd.mu.Lock()
	defer s.upd.mu.Unlock()
	return s.upd.restart
}
