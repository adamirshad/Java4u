package core

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeModJar(t *testing.T, path, meta string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	if meta != "" {
		w, _ := zw.Create("fabric.mod.json")
		w.Write([]byte(meta))
	}
	zw.Close()
	f.Close()
}

func TestModsFolder(t *testing.T) {
	dir := t.TempDir()
	writeModJar(t, filepath.Join(dir, "sodium.jar"), `{"schemaVersion":1,"id":"sodium","name":"Sodium","version":"0.9.2"}`)
	writeModJar(t, filepath.Join(dir, "plain.jar"), "")
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644)

	mods, err := ListMods(dir)
	if err != nil || len(mods) != 2 {
		t.Fatalf("list: %v %+v", err, mods)
	}
	var sodium InstalledMod
	for _, m := range mods {
		if m.File == "sodium.jar" {
			sodium = m
		}
	}
	if sodium.ID != "sodium" || sodium.Version != "0.9.2" || !sodium.Enabled {
		t.Fatalf("metadata: %+v", sodium)
	}

	if err := SetModEnabled(dir, "sodium.jar", false); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(dir, "sodium.jar.disabled")) {
		t.Fatal("not disabled")
	}
	mods, _ = ListMods(dir)
	if i := slices.IndexFunc(mods, func(m InstalledMod) bool { return m.File == "sodium.jar" }); i < 0 || mods[i].Enabled {
		t.Fatal("disabled mod should still be listed, as off")
	}
	if err := SetModEnabled(dir, "sodium.jar", true); err != nil || !exists(filepath.Join(dir, "sodium.jar")) {
		t.Fatal("re-enable failed")
	}

	for _, bad := range []string{"../x.jar", "a/b.jar", ".hidden.jar", "x.zip", ""} {
		if SetModEnabled(dir, bad, true) == nil || RemoveMod(dir, bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if err := RemoveMod(dir, "sodium.jar"); err != nil || exists(filepath.Join(dir, "sodium.jar")) {
		t.Fatal("remove failed")
	}
	if RemoveMod(dir, "sodium.jar") == nil {
		t.Fatal("removing twice should fail")
	}
}

func TestSetAllModsEnabled(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.jar", "b.jar", "c.jar"} {
		writeModJar(t, filepath.Join(dir, n), "")
	}
	SetModEnabled(dir, "b.jar", false)
	if n, err := SetAllModsEnabled(dir, false); err != nil || n != 2 {
		t.Fatalf("off: %d %v", n, err)
	}
	mods, _ := ListMods(dir)
	for _, m := range mods {
		if m.Enabled {
			t.Fatalf("%s still on", m.File)
		}
	}
	if n, err := SetAllModsEnabled(dir, true); err != nil || n != 3 {
		t.Fatalf("on: %d %v", n, err)
	}
	if n, _ := SetAllModsEnabled(t.TempDir(), true); n != 0 {
		t.Fatal("empty folder")
	}
}

func TestModIndex(t *testing.T) {
	dir := t.TempDir()
	idx := loadIndex(dir)
	idx.Files["a.jar"] = indexEntry{ProjectID: "P1", VersionID: "V1"}
	if err := idx.save(dir); err != nil {
		t.Fatal(err)
	}
	back := loadIndex(dir)
	if f, ok := back.projectFile("P1"); !ok || f != "a.jar" {
		t.Fatalf("index round trip: %+v", back)
	}
	data, _ := os.ReadFile(filepath.Join(dir, modIndexFile))
	var raw map[string]any
	if json.Unmarshal(data, &raw) != nil {
		t.Fatal("index is not JSON")
	}
}

func TestBuildCommandWithChunky(t *testing.T) {
	dirs := NewDirs(t.TempDir())
	os.MkdirAll(dirs.AssetIndexesDir(), 0o755)
	os.WriteFile(dirs.AssetIndex("17"), []byte(`{"objects":{}}`), 0o644)
	js := `{"id":"26.3","type":"release","mainClass":"net.minecraft.client.main.Main",
	  "assetIndex":{"id":"17"},"downloads":{"client":{}},
	  "arguments":{"jvm":["-cp","${classpath}"],"game":["--username","${auth_player_name}"]}}`
	var v VersionJSON
	json.Unmarshal([]byte(js), &v)
	a, _ := OfflineAccount("Steve")
	game := filepath.Join(dirs.Root, "game")
	cmd, err := BuildCommand(dirs, &v, LaunchOptions{
		Java: "java", GameDir: game, Account: a, MemoryMB: 2048,
		Chunky: &ChunkyLaunch{Jar: "chunky.jar", Libraries: []string{"mixin.jar"}, MainClass: "dev.java4u.chunky.Main"},
	})
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(cmd.Args, " ")
	sep := string(os.PathListSeparator)
	if !strings.Contains(args, "-cp chunky.jar"+sep+"mixin.jar ") {
		t.Fatalf("classpath should be Chunky first: %s", args)
	}
	if !strings.Contains(args, "-Dchunky.mainClass=net.minecraft.client.main.Main") ||
		!strings.Contains(args, " dev.java4u.chunky.Main --username Steve") {
		t.Fatalf("Chunky main/props missing: %s", args)
	}
	list, err := os.ReadFile(filepath.Join(game, ".chunky", "classpath.txt"))
	if err != nil || !strings.Contains(string(list), dirs.VersionJar("26.3")) {
		t.Fatalf("game classpath file: %v %q", err, list)
	}
}
