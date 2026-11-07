package gui

import (
	"net/http"

	"github.com/adamirshad/Java4u/java4u-go/internal/core"
)

// horizonAPI is where Horizon's releases are listed. Tests change it.
var horizonAPI = core.HorizonReleasesURL

// horizonInfo tells the app whether Horizon can run here and which version is installed.
func horizonInfo(dirs core.Dirs) map[string]any {
	installed := ""
	if info := core.InstalledHorizon(dirs); info != nil {
		installed = info.Version
	}
	return map[string]any{
		"supported": core.HorizonSupported(),
		"installed": installed,
		"page":      core.HorizonPage,
	}
}

// startHorizon installs or updates Horizon's launcher in the background, then opens it.
// Horizon's launcher signs the player in and runs the game itself.
func (s *Server) startHorizon(w http.ResponseWriter) {
	if !core.HorizonSupported() {
		writeErr(w, http.StatusBadRequest, "Horizon has no launcher for this computer.")
		return
	}
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		writeErr(w, http.StatusConflict, "Already running.")
		return
	}
	s.busy = true
	s.task = "horizon"
	s.progress = nil
	s.log = nil
	s.logStart = 0
	s.logEpoch++
	s.mu.Unlock()

	go func() {
		info, err := core.EnsureHorizon(s.ctx, s.client, s.dirs, horizonAPI, s.setProgress, s.logLine)
		if err == nil {
			err = core.OpenHorizon(s.dirs)
		}
		s.mu.Lock()
		s.busy = false
		s.task = ""
		s.progress = nil
		s.mu.Unlock()
		if err != nil {
			s.logLine("Error: " + err.Error())
			return
		}
		s.logLine("Opened the Horizon launcher " + info.Version + ". Sign in there to play.")
	}()
	writeJSON(w, map[string]bool{"ok": true})
}
