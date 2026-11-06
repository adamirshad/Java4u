package core

// Performance helpers: sensible memory sizing and JVM flag presets.

import (
	"fmt"
	"strings"
)

type Profile string

const (
	// ProfileOff adds no flags; the JVM picks its own defaults.
	ProfileOff Profile = "off"
	// ProfileBalanced uses G1 with sane pause targets. Safe everywhere.
	ProfileBalanced Profile = "balanced"
	// ProfilePerformance uses tuned G1 flags that smooth out stutter on heavier modded setups.
	ProfilePerformance Profile = "performance"
)

var AllProfiles = []Profile{ProfileOff, ProfileBalanced, ProfilePerformance}

func (p Profile) Label() string {
	switch p {
	case ProfileOff:
		return "Off"
	case ProfilePerformance:
		return "Performance"
	}
	return "Balanced"
}

func (p Profile) Description() string {
	switch p {
	case ProfileOff:
		return "No extra flags; the JVM uses its own defaults."
	case ProfilePerformance:
		return "Tuned G1 flags that reduce stutter, best for modded play."
	}
	return "G1 garbage collector with short pauses. Safe for everything."
}

func ParseProfile(s string) (Profile, error) {
	switch strings.ToLower(s) {
	case "off", "none":
		return ProfileOff, nil
	case "balanced", "":
		return ProfileBalanced, nil
	case "performance", "perf":
		return ProfilePerformance, nil
	}
	return "", fmt.Errorf("unknown profile '%s' (use off, balanced or performance)", s)
}

// TotalMemoryMB is this machine's physical RAM, or 0 if it cannot be read.
func TotalMemoryMB() int {
	return int(totalMemoryBytes() / 1024 / 1024)
}

// RecommendedMemoryMB is a good default heap size for this machine: never more than half the RAM.
func RecommendedMemoryMB() int {
	total := TotalMemoryMB()
	if total == 0 {
		return 2048
	}
	var tier int
	switch {
	case total <= 4096:
		tier = 2048
	case total <= 8192:
		tier = 3072
	case total <= 16384:
		tier = 4096
	default:
		tier = 6144
	}
	return max(min(tier, total/2), 1024)
}

// MaxMemoryMB is the upper bound offered in memory sliders: 75% of RAM.
func MaxMemoryMB() int {
	return max(TotalMemoryMB()*3/4, 2048)
}

var balancedFlags = []string{
	"-XX:+UseG1GC",
	"-XX:+ParallelRefProcEnabled",
	"-XX:MaxGCPauseMillis=200",
}

var performanceExtra = []string{
	"-XX:+UnlockExperimentalVMOptions",
	"-XX:+DisableExplicitGC",
	"-XX:G1NewSizePercent=30",
	"-XX:G1MaxNewSizePercent=40",
	"-XX:G1HeapRegionSize=8M",
	"-XX:G1ReservePercent=20",
	"-XX:G1HeapWastePercent=5",
	"-XX:G1MixedGCCountTarget=4",
	"-XX:InitiatingHeapOccupancyPercent=15",
	"-XX:G1MixedGCLiveThresholdPercent=90",
	"-XX:G1RSetUpdatingPauseTimePercent=5",
	"-XX:SurvivorRatio=32",
	"-XX:+PerfDisableSharedMem",
	"-XX:MaxTenuringThreshold=1",
}

// JVMFlags returns the flags for a profile. Respects the user's own -Xms and GC choices.
func JVMFlags(p Profile, memoryMB int, userArgs []string) []string {
	if p == ProfileOff {
		return nil
	}
	var flags []string
	hasXms, hasGC := false, false
	for _, a := range userArgs {
		if strings.HasPrefix(a, "-Xms") {
			hasXms = true
		}
		if strings.HasPrefix(a, "-XX:+Use") && strings.HasSuffix(a, "GC") {
			hasGC = true
		}
	}
	if !hasXms {
		flags = append(flags, fmt.Sprintf("-Xms%dM", min(memoryMB, 1024)))
	}
	if hasGC {
		return flags
	}
	flags = append(flags, balancedFlags...)
	if p == ProfilePerformance {
		flags = append(flags, performanceExtra...)
	}
	return flags
}
