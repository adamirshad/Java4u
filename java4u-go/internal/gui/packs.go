package gui

// Modpack installs from Modrinth or from a .mrpack file, and exporting an instance as one.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adamirshad/Java4u/java4u-go/internal/chunky"
	"github.com/adamirshad/Java4u/java4u-go/internal/core"
)

// installPack runs a pack install in the background, reporting progress and the result
// in the status line.
func (s *Server) installPack(w http.ResponseWriter, title string, install func(ctx context.Context) (*core.Instance, error)) {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		writeErr(w, http.StatusConflict, "Wait for the current download or game to finish.")
		return
	}
	s.busy = true
	s.task = "modpack"
	s.progress = nil
	s.status = "Installing " + title + "..."
	s.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 30*time.Minute)
		defer cancel()
		inst, err := install(ctx)
		s.mu.Lock()
		s.busy = false
		s.task = ""
		s.progress = nil
		if err != nil {
			s.status = "Could not install " + title + ": " + err.Error()
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

// handleImportMrpack installs an uploaded .mrpack file as a new instance.
func (s *Server) handleImportMrpack(w http.ResponseWriter, r *http.Request) {
	if !chunkyAvailable() {
		writeErr(w, http.StatusBadRequest, chunky.ErrNotBuilt.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<30)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "Could not read that file: "+err.Error())
		return
	}
	defer file.Close()
	if !strings.EqualFold(filepath.Ext(header.Filename), ".mrpack") {
		writeErr(w, http.StatusBadRequest, "Choose a Modrinth pack (.mrpack) file.")
		return
	}
	tmp, err := os.CreateTemp("", "java4u-import-*.mrpack")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, err = io.Copy(tmp, file)
	tmp.Close()
	if err != nil {
		os.Remove(tmp.Name())
		writeErr(w, http.StatusBadRequest, "Could not read that file: "+err.Error())
		return
	}
	name := strings.TrimSuffix(filepath.Base(header.Filename), filepath.Ext(header.Filename))
	s.installPack(w, name, func(ctx context.Context) (*core.Instance, error) {
		defer os.Remove(tmp.Name())
		return core.InstallMrpack(ctx, s.client, s.dirs, tmp.Name(), name, "", s.setProgress)
	})
}

// handleExportMrpack builds a .mrpack of an instance and sends it as a download.
func (s *Server) handleExportMrpack(w http.ResponseWriter, r *http.Request) {
	inst, err := core.LoadInstance(s.dirs, r.PathValue("name"))
	if err != nil {
		http.Error(w, "No such instance.", http.StatusNotFound)
		return
	}
	dir, err := os.MkdirTemp("", "java4u-export")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(dir)
	dest := filepath.Join(dir, "pack.mrpack")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	linked, bundled, err := core.ExportMrpack(ctx, s.client, s.dirs, inst, dest)
	if err != nil {
		http.Error(w, "Could not export: "+err.Error(), http.StatusInternalServerError)
		return
	}
	f, err := os.Open(dest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	s.setStatus(fmt.Sprintf("Exported \"%s\": %d mods from Modrinth, %d bundled in the pack.", inst.Name, linked, bundled))
	w.Header().Set("Content-Type", "application/x-modrinth-modpack+zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.mrpack"`, safeFileName(inst.Name)))
	io.Copy(w, f)
}

// safeFileName keeps letters, digits, spaces, dots, dashes and underscores.
func safeFileName(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == ' ', r == '.', r == '-', r == '_':
			return r
		}
		return '_'
	}, name)
}
