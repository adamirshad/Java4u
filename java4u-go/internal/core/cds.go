package core

// Faster JVM start-up for the game: the base class data sharing (CDS) archive.
//
// Ordinary JDKs ship lib/server/classes.jsa, a ready-made archive of the JDK's own
// classes that every JVM maps in at start instead of loading those classes one by one.
// Mojang's Java runtimes leave it out, so Java4U builds it once per runtime with
// `java -Xshare:dump` (under a second). Vanilla 26.3 reached the title screen about 10%
// sooner with it (8.0 s -> 7.2 s).
//
// Per-game archives (-XX:+AutoCreateSharedArchive, or Java 25's AOT cache) would save a
// little more on later launches, but recording them made the first launch of Minecraft
// over ten times slower, so Java4U doesn't use them.

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ensureBaseCDS builds the base class archive for a Java runtime that Java4U downloaded.
// A Java the player chose themselves is never changed.
func ensureBaseCDS(dirs Dirs, java string) {
	rel, err := filepath.Rel(dirs.RuntimesDir(), java)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) || os.Getenv("JAVA4U_NO_CDS") != "" {
		return
	}
	marker := filepath.Join(filepath.Dir(java), ".java4u-cds")
	if exists(marker) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, java, "-Xshare:dump")
	HideConsole(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("could not build the Java class archive (the game still starts normally): %v: %s", err, strings.TrimSpace(string(out)))
	}
	// Try once per runtime either way; a failure only means a slightly slower start.
	os.WriteFile(marker, nil, 0o644)
}
