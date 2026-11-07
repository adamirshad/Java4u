package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManifestCacheRoundTrip(t *testing.T) {
	dirs := Dirs{Root: t.TempDir()}
	if _, err := LoadCachedManifest(dirs); err == nil {
		t.Fatal("expected no cache yet")
	}
	m := &Manifest{Latest: Latest{Release: "26.3"}, Versions: []VersionEntry{{ID: "26.3", Kind: "release"}}}
	if err := SaveCachedManifest(dirs, m); err != nil {
		t.Fatal(err)
	}
	back, err := LoadCachedManifest(dirs)
	if err != nil || back.Latest.Release != "26.3" || len(back.Versions) != 1 {
		t.Fatalf("%+v %v", back, err)
	}
}

// Java4U only adds the class archive to Java runtimes it downloaded itself.
func TestBaseCDSLeavesOtherJavaAlone(t *testing.T) {
	dirs := Dirs{Root: t.TempDir()}
	own := filepath.Join(t.TempDir(), "bin", "java")
	os.MkdirAll(filepath.Dir(own), 0o755)
	os.WriteFile(own, []byte("not really java"), 0o755)

	ensureBaseCDS(dirs, own)
	if exists(filepath.Join(filepath.Dir(own), ".java4u-cds")) {
		t.Fatal("touched a Java outside Java4U's runtimes folder")
	}

	// Inside the runtimes folder it tries once and remembers, even when the dump fails.
	managed := filepath.Join(dirs.RuntimesDir(), "java-runtime-x", "bin", "java")
	os.MkdirAll(filepath.Dir(managed), 0o755)
	os.WriteFile(managed, []byte("not really java"), 0o755)
	ensureBaseCDS(dirs, managed)
	if !exists(filepath.Join(filepath.Dir(managed), ".java4u-cds")) {
		t.Fatal("managed runtime not marked as done")
	}
}
