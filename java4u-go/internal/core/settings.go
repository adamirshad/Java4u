package core

import (
	"encoding/json"
	"os"
)

type Settings struct {
	// Wallpaper is a file name inside the wallpapers directory, or empty.
	Wallpaper string `json:"wallpaper,omitempty"`
	// WallpaperDim is 0.0 (no darkening) to 0.9; keeps text readable on bright images.
	WallpaperDim    float64 `json:"wallpaper_dim"`
	OfflineUsername string  `json:"offline_username"`
	LastInstance    string  `json:"last_instance,omitempty"`
	ShowSnapshots   bool    `json:"show_snapshots"`
	// PlayOffline launches with OfflineUsername instead of a Microsoft account (testing only).
	PlayOffline bool   `json:"play_offline"`
	LastAccount string `json:"last_account,omitempty"`
}

func DefaultSettings() Settings {
	return Settings{WallpaperDim: 0.45, OfflineUsername: "Player"}
}

// LoadSettings falls back to defaults on missing or unreadable settings rather than blocking startup.
func LoadSettings(dirs Dirs) Settings {
	s := DefaultSettings()
	data, err := os.ReadFile(dirs.SettingsFile())
	if err != nil {
		return s
	}
	if json.Unmarshal(data, &s) != nil {
		return DefaultSettings()
	}
	s.WallpaperDim = min(max(s.WallpaperDim, 0), 0.9)
	return s
}

func (s Settings) Save(dirs Dirs) error {
	if err := os.MkdirAll(dirs.Root, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dirs.SettingsFile(), data, 0o644)
}
