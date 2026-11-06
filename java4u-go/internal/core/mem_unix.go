//go:build !windows

package core

import (
	"bufio"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

func totalMemoryBytes() uint64 {
	switch runtime.GOOS {
	case "linux":
		f, err := os.Open("/proc/meminfo")
		if err != nil {
			return 0
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) >= 2 && fields[0] == "MemTotal:" {
				kb, err := strconv.ParseUint(fields[1], 10, 64)
				if err != nil {
					return 0
				}
				return kb * 1024
			}
		}
	case "darwin", "freebsd", "openbsd", "netbsd":
		key := "hw.physmem"
		if runtime.GOOS == "darwin" {
			key = "hw.memsize"
		}
		out, err := exec.Command("sysctl", "-n", key).Output()
		if err != nil {
			return 0
		}
		n, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
		if err != nil {
			return 0
		}
		return n
	}
	return 0
}

// HideConsole is only needed on Windows.
func HideConsole(cmd *exec.Cmd) {}
