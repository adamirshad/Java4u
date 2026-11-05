package core

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func javaExe() string {
	if runtime.GOOS == "windows" {
		return "java.exe"
	}
	return "java"
}

// FindJava returns JAVA_HOME/bin/java if set, otherwise `java` from PATH.
func FindJava() string {
	if home := os.Getenv("JAVA_HOME"); home != "" {
		p := filepath.Join(home, "bin", javaExe())
		if exists(p) {
			return p
		}
	}
	return javaExe()
}

// JavaMajor runs `java -version` and returns the major version.
func JavaMajor(java string) (int, error) {
	cmd := exec.Command(java, "-version")
	HideConsole(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return 0, fmt.Errorf("could not run %s: %w", java, err)
	}
	major, ok := parseMajor(string(out))
	if !ok {
		return 0, errors.New("could not parse `java -version` output")
	}
	return major, nil
}

func parseMajor(text string) (int, bool) {
	start := strings.IndexByte(text, '"')
	if start < 0 {
		return 0, false
	}
	rest := text[start+1:]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return 0, false
	}
	ver := rest[:end]
	var major string
	if r, ok := strings.CutPrefix(ver, "1."); ok {
		major, _, _ = strings.Cut(r, ".")
	} else {
		n := 0
		for n < len(ver) && ver[n] >= '0' && ver[n] <= '9' {
			n++
		}
		major = ver[:n]
	}
	v, err := strconv.Atoi(major)
	return v, err == nil
}

func CheckJava(java string, required int) error {
	have, err := JavaMajor(java)
	if err != nil {
		return err
	}
	if have < required {
		return fmt.Errorf("this Minecraft version needs Java %d+, but %s is Java %d", required, java, have)
	}
	return nil
}
