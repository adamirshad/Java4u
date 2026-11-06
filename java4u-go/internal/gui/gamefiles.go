package gui

// Worlds and screenshots of an instance, and opening its folders.

import (
	"net/http"

	"github.com/adamirshad/Java4u/java4u-go/internal/core"
)

func (s *Server) instanceFromPath(w http.ResponseWriter, r *http.Request) *core.Instance {
	inst, err := core.LoadInstance(s.dirs, r.PathValue("name"))
	if err != nil {
		http.Error(w, "No such instance.", http.StatusNotFound)
		return nil
	}
	return inst
}

func (s *Server) handleGameFiles(w http.ResponseWriter, r *http.Request) {
	inst := s.instanceFromPath(w, r)
	if inst == nil {
		return
	}
	worlds, err := core.ListWorlds(s.dirs, inst)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	shots, err := core.ListScreenshots(s.dirs, inst)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"worlds": worlds, "screenshots": shots})
}

func (s *Server) serveImage(w http.ResponseWriter, r *http.Request, path string, err error) {
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeFile(w, r, path)
}

func (s *Server) handleScreenshot(w http.ResponseWriter, r *http.Request) {
	if inst := s.instanceFromPath(w, r); inst != nil {
		p, err := core.ScreenshotPath(s.dirs, inst, r.PathValue("file"))
		s.serveImage(w, r, p, err)
	}
}

func (s *Server) handleWorldIcon(w http.ResponseWriter, r *http.Request) {
	if inst := s.instanceFromPath(w, r); inst != nil {
		p, err := core.WorldIconPath(s.dirs, inst, r.PathValue("folder"))
		s.serveImage(w, r, p, err)
	}
}

// handleOpenFolder opens one of an instance's folders in the file manager.
func (s *Server) handleOpenFolder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Instance string `json:"instance"`
		Folder   string `json:"folder"` // "game", "saves", "screenshots" or "mods"
	}
	if !readJSON(w, r, &req) {
		return
	}
	inst, err := core.LoadInstance(s.dirs, req.Instance)
	if err != nil {
		writeErr(w, http.StatusNotFound, "No such instance.")
		return
	}
	dirs := map[string]string{
		"game":        inst.GameDir(s.dirs),
		"saves":       inst.SavesDir(s.dirs),
		"screenshots": inst.ScreenshotsDir(s.dirs),
		"mods":        inst.ModsDir(s.dirs),
	}
	dir, ok := dirs[req.Folder]
	if !ok {
		writeErr(w, http.StatusBadRequest, "unknown folder")
		return
	}
	if err := core.OpenFolder(dir); err != nil {
		writeErr(w, http.StatusInternalServerError, "Could not open the folder: "+err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
