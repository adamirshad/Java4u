package core

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEULA(t *testing.T) {
	dir := t.TempDir()
	if EULAAccepted(dir) {
		t.Fatal("no file means not accepted")
	}
	os.WriteFile(filepath.Join(dir, "eula.txt"), []byte("#comment\neula=false\n"), 0o644)
	if EULAAccepted(dir) {
		t.Fatal("eula=false accepted")
	}
	if err := AcceptEULA(dir); err != nil || !EULAAccepted(dir) {
		t.Fatal("AcceptEULA didn't stick")
	}
}

func TestServers(t *testing.T) {
	dirs := NewDirs(t.TempDir())
	if _, err := CreateServer(dirs, "../x", "26.3"); err == nil {
		t.Fatal("bad name accepted")
	}
	s, err := CreateServer(dirs, "Friends", "26.3")
	if err != nil {
		t.Fatal(err)
	}
	s.Loader = LoaderChunky
	s.Save(dirs)
	back, err := LoadServer(dirs, "Friends")
	if err != nil || back.Loader != LoaderChunky || back.MemoryMB != 2048 {
		t.Fatalf("round trip: %+v %v", back, err)
	}
	if len(ListServers(dirs)) != 1 || DeleteServer(dirs, "Friends") != nil || len(ListServers(dirs)) != 0 {
		t.Fatal("list/delete")
	}
}

func sum256(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestUnpackServer(t *testing.T) {
	dirs := NewDirs(t.TempDir())
	server, lib := []byte("server jar"), []byte("library jar")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, data []byte) { w, _ := zw.Create(name); w.Write(data) }
	add("META-INF/main-class", []byte("net.minecraft.server.Main\n"))
	add("META-INF/versions.list", []byte(sum256(server)+"\t26.3\t26.3/server-26.3.jar\n"))
	add("META-INF/libraries.list", []byte(sum256(lib)+"\tcom.example:lib:1\tcom/example/lib/1/lib-1.jar\n"))
	add("META-INF/versions/26.3/server-26.3.jar", server)
	add("META-INF/libraries/com/example/lib/1/lib-1.jar", lib)
	zw.Close()
	bundler := filepath.Join(dirs.Root, "bundler.jar")
	os.WriteFile(bundler, buf.Bytes(), 0o644)

	files, err := unpackServer(dirs, "26.3", bundler, "abc", nil)
	if err != nil {
		t.Fatal(err)
	}
	if files.MainClass != "net.minecraft.server.Main" || len(files.Libraries) != 1 {
		t.Fatalf("files: %+v", files)
	}
	if got, _ := os.ReadFile(files.Jar); string(got) != "server jar" {
		t.Fatal("server jar not extracted")
	}

	// A tampered entry must be rejected.
	var bad bytes.Buffer
	zw = zip.NewWriter(&bad)
	add = func(name string, data []byte) { w, _ := zw.Create(name); w.Write(data) }
	add("META-INF/main-class", []byte("x"))
	add("META-INF/versions.list", []byte(sum256(server)+"\t26.4\t26.4/server.jar\n"))
	add("META-INF/libraries.list", []byte(""))
	add("META-INF/versions/26.4/server.jar", []byte("tampered"))
	zw.Close()
	os.WriteFile(bundler, bad.Bytes(), 0o644)
	if _, err := unpackServer(dirs, "26.4", bundler, "def", nil); err == nil {
		t.Fatal("tampered server jar accepted")
	}
}

func TestMrpackSafety(t *testing.T) {
	dirs := NewDirs(t.TempDir())
	write := func(index string, extra map[string]string) string {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create("modrinth.index.json")
		w.Write([]byte(index))
		for name, body := range extra {
			w, _ := zw.Create(name)
			w.Write([]byte(body))
		}
		zw.Close()
		p := filepath.Join(t.TempDir(), "p.mrpack")
		os.WriteFile(p, buf.Bytes(), 0o644)
		return p
	}
	ctx := context.Background()
	old := write(`{"formatVersion":1,"game":"minecraft","name":"Old","files":[],"dependencies":{"minecraft":"1.21.1","fabric-loader":"0.16.0"}}`, nil)
	if _, err := InstallMrpack(ctx, NewClient(), dirs, old, "Old", "", nil); err == nil {
		t.Fatal("pre-26 pack accepted")
	}
	forge := write(`{"formatVersion":1,"game":"minecraft","name":"F","files":[],"dependencies":{"minecraft":"26.3","neoforge":"1"}}`, nil)
	if _, err := InstallMrpack(ctx, NewClient(), dirs, forge, "F", "", nil); err == nil {
		t.Fatal("NeoForge pack accepted")
	}
	escape := write(`{"formatVersion":1,"game":"minecraft","name":"E","files":[{"path":"../../evil.jar","hashes":{"sha1":"0000000000000000000000000000000000000000"},"downloads":["https://cdn.modrinth.com/x.jar"]}],"dependencies":{"minecraft":"26.3"}}`, nil)
	if _, err := InstallMrpack(ctx, NewClient(), dirs, escape, "E", "", nil); err == nil {
		t.Fatal("escaping path accepted")
	}
	if insts, _ := ListInstances(dirs); len(insts) != 0 {
		t.Fatalf("failed installs left instances behind: %v", insts)
	}
	ok := write(`{"formatVersion":1,"game":"minecraft","name":"Cozy","files":[],"dependencies":{"minecraft":"26.3","fabric-loader":"0.17.0"}}`,
		map[string]string{"overrides/config/a.txt": "hello", "client-overrides/config/a.txt": "client wins"})
	inst, err := InstallMrpack(ctx, NewClient(), dirs, ok, "Cozy: Pack!", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if inst.Name != "Cozy Pack" || inst.Loader != LoaderChunky || inst.Modpack != "Cozy" {
		t.Fatalf("instance: %+v", inst)
	}
	got, _ := os.ReadFile(filepath.Join(inst.GameDir(dirs), "config", "a.txt"))
	if string(got) != "client wins" {
		t.Fatalf("overrides: %q", got)
	}
	second, _ := InstallMrpack(ctx, NewClient(), dirs, ok, "Cozy: Pack!", "", nil)
	if second == nil || !strings.HasSuffix(second.Name, " 2") {
		t.Fatalf("second install should get a new name, got %+v", second)
	}
}
