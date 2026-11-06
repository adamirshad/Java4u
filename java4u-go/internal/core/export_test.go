package core

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeJar(t *testing.T, path, id string) []byte {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	f, _ := os.Create(path)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("fabric.mod.json")
	w.Write([]byte(`{"id":"` + id + `","name":"` + id + `","version":"1.0"}`))
	zw.Close()
	f.Close()
	data, _ := os.ReadFile(path)
	return data
}

func TestExportMrpack(t *testing.T) {
	dirs := Dirs{Root: t.TempDir()}
	inst, err := CreateInstance(dirs, "Pack Test", "26.1")
	if err != nil {
		t.Fatal(err)
	}
	mods := inst.ModsDir(dirs)
	known := writeJar(t, filepath.Join(mods, "sodium.jar"), "sodium")
	writeJar(t, filepath.Join(mods, "homemade.jar"), "homemade")
	writeJar(t, filepath.Join(mods, "off.jar"), "off")
	SetModEnabled(mods, "off.jar", false)
	os.MkdirAll(filepath.Join(inst.GameDir(dirs), "config"), 0o755)
	os.WriteFile(filepath.Join(inst.GameDir(dirs), "config", "sodium.json"), []byte("{}"), 0o644)

	sum := sha1.Sum(known)
	knownHash := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version_files":
			json.NewEncoder(w).Encode(map[string]any{knownHash: map[string]any{
				"files": []map[string]any{{"url": "https://cdn.modrinth.com/data/AANobbMI/versions/x/sodium.jar", "hashes": map[string]string{"sha1": knownHash}}},
			}})
		case "/loader":
			w.Write([]byte(`[{"version":"0.18.0","stable":false},{"version":"0.17.9","stable":true}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "out.mrpack")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	linked, bundled, err := exportMrpack(ctx, srv.Client(), dirs, inst, dest, exportSources{srv.URL, srv.URL + "/loader"})
	if err != nil {
		t.Fatal(err)
	}
	if linked != 1 || bundled != 1 {
		t.Fatalf("linked %d bundled %d", linked, bundled)
	}

	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var idx packIndex
	if err := readZipJSON(&zr.Reader, "modrinth.index.json", &idx); err != nil {
		t.Fatal(err)
	}
	if idx.FormatVersion != 1 || idx.Dependencies["minecraft"] != "26.1" || idx.Dependencies["fabric-loader"] != "0.17.9" {
		t.Fatalf("index: %+v", idx)
	}
	if len(idx.Files) != 1 || idx.Files[0].Path != "mods/sodium.jar" || idx.Files[0].Hashes["sha1"] != knownHash || len(idx.Files[0].Hashes["sha512"]) != 128 {
		t.Fatalf("files: %+v", idx.Files)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{"overrides/mods/homemade.jar", "overrides/config/sodium.json"} {
		if !names[want] {
			t.Errorf("missing %s in %v", want, names)
		}
	}
	if names["overrides/mods/off.jar"] || names["overrides/mods/off.jar.disabled"] {
		t.Error("disabled mod was exported")
	}
}

func TestWorldsAndScreenshots(t *testing.T) {
	dirs := Dirs{Root: t.TempDir()}
	inst, _ := CreateInstance(dirs, "W", "26.1")
	saves := inst.SavesDir(dirs)
	os.MkdirAll(filepath.Join(saves, "Old"), 0o755)
	os.WriteFile(filepath.Join(saves, "Old", "level.dat"), []byte("12345"), 0o644)
	os.MkdirAll(filepath.Join(saves, "New"), 0o755)
	os.WriteFile(filepath.Join(saves, "New", "level.dat"), []byte("1"), 0o644)
	os.WriteFile(filepath.Join(saves, "New", "icon.png"), []byte("png"), 0o644)
	os.MkdirAll(filepath.Join(saves, "junk"), 0o755) // no level.dat: not a world
	past := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(saves, "Old", "level.dat"), past, past)

	worlds, err := ListWorlds(dirs, inst)
	if err != nil || len(worlds) != 2 || worlds[0].Folder != "New" || !worlds[0].HasIcon || worlds[1].SizeBytes != 5 {
		t.Fatalf("%+v %v", worlds, err)
	}

	shots := inst.ScreenshotsDir(dirs)
	os.MkdirAll(shots, 0o755)
	os.WriteFile(filepath.Join(shots, "a.png"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(shots, "notes.txt"), []byte("x"), 0o644)
	list, _ := ListScreenshots(dirs, inst)
	if len(list) != 1 || list[0].File != "a.png" {
		t.Fatalf("%+v", list)
	}
	for _, bad := range []string{"../a.png", `..\a.png`, "C:a.png", "a.txt", ""} {
		if _, err := ScreenshotPath(dirs, inst, bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, err := WorldIconPath(dirs, inst, ".."); err == nil {
		t.Error(".. accepted as a world")
	}
}
