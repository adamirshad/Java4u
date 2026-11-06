package gui

// Mod management for the app: list, install from Modrinth, enable/disable and remove.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/adamirshad/Java4u/java4u-go/internal/chunky"
	"github.com/adamirshad/Java4u/java4u-go/internal/core"
)

func chunkyAvailable() bool {
	_, ok := chunky.Jar()
	return ok
}

func (s *Server) handleListMods(w http.ResponseWriter, r *http.Request) {
	inst, err := core.LoadInstance(s.dirs, r.URL.Query().Get("instance"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	mods, err := core.ListMods(inst.ModsDir(s.dirs))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"instance":         inst.Name,
		"version":          inst.Version,
		"loader":           inst.Loader,
		"chunky_supported": chunky.SupportsVersion(inst.Version),
		"mods":             mods,
	})
}

func (s *Server) handleInstallMod(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Instance string `json:"instance"`
		Project  string `json:"project"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	inst, err := core.LoadInstance(s.dirs, req.Instance)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !chunkyAvailable() {
		writeErr(w, http.StatusBadRequest, chunky.ErrNotBuilt.Error())
		return
	}
	switched := false
	if inst.Loader != core.LoaderChunky {
		if !chunky.SupportsVersion(inst.Version) {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf(
				"Mods run on Chunky, which supports Minecraft 26.1 and newer. %s is Minecraft %s.", inst.Name, inst.Version))
			return
		}
		inst.Loader = core.LoaderChunky
		if err := inst.Save(s.dirs); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		switched = true
	}

	s.setStatus("Installing " + req.Project + "...")
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	defer cancel()
	files, err := core.InstallMod(ctx, s.client, inst.ModsDir(s.dirs), req.Project, inst.Version)
	if err != nil {
		s.setStatus("Could not install: " + err.Error())
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, strings.TrimSuffix(f.File, ".jar"))
	}
	msg := "Already installed."
	if len(names) > 0 {
		msg = "Installed " + strings.Join(names, ", ") + " into " + inst.Name + "."
	}
	if switched {
		msg += " " + inst.Name + " now uses Chunky."
	}
	s.setStatus(msg)
	writeJSON(w, map[string]any{"installed": files, "switched": switched, "message": msg})
}

func (s *Server) handleToggleMod(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Instance string `json:"instance"`
		File     string `json:"file"`
		Enabled  bool   `json:"enabled"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	inst, err := core.LoadInstance(s.dirs, req.Instance)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := core.SetModEnabled(inst.ModsDir(s.dirs), req.File, req.Enabled); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleRemoveMod(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Instance string `json:"instance"`
		File     string `json:"file"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	inst, err := core.LoadInstance(s.dirs, req.Instance)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := core.RemoveMod(inst.ModsDir(s.dirs), req.File); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.setStatus("Removed " + req.File + ".")
	writeJSON(w, map[string]bool{"ok": true})
}
