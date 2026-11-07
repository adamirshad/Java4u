package gui

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/adamirshad/Java4u/java4u-go/internal/core"
)

func TestGameSetting(t *testing.T) {
	s, url, c := start(t)
	base := "http://" + s.host
	if _, err := c.Get(url); err != nil {
		t.Fatal(err)
	}

	if resp := postJSON(t, c, base+"/api/settings", map[string]string{"game": "bedrock"}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown game: got %d", resp.StatusCode)
	}
	resp := postJSON(t, c, base+"/api/settings", map[string]string{"game": core.GameHorizon})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("horizon: got %d", resp.StatusCode)
	}
	if got := core.LoadSettings(s.dirs).Game; got != core.GameHorizon {
		t.Fatalf("saved game %q", got)
	}

	resp, err := c.Get(base + "/api/data")
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Horizon struct {
			Supported bool   `json:"supported"`
			Installed string `json:"installed"`
		} `json:"horizon"`
	}
	json.NewDecoder(resp.Body).Decode(&data)
	resp.Body.Close()
	if data.Horizon.Supported != core.HorizonSupported() || data.Horizon.Installed != "" {
		t.Fatalf("horizon info: %+v", data.Horizon)
	}
}

func TestPlayHorizonReportsFailure(t *testing.T) {
	if !core.HorizonSupported() {
		t.Skip("Horizon has no launcher for this platform")
	}
	s, url, c := start(t)
	base := "http://" + s.host
	if _, err := c.Get(url); err != nil {
		t.Fatal(err)
	}
	old := horizonAPI
	horizonAPI = "http://127.0.0.1:1/unreachable"
	defer func() { horizonAPI = old }()

	if resp := postJSON(t, c, base+"/api/play", map[string]string{"game": core.GameHorizon}); resp.StatusCode != http.StatusOK {
		t.Fatalf("play: got %d", resp.StatusCode)
	}
	// Wait for the background task to finish and report its error.
	for range 200 {
		s.mu.Lock()
		busy, log := s.busy, append([]string{}, s.log...)
		s.mu.Unlock()
		if !busy && len(log) > 0 {
			if last := log[len(log)-1]; !strings.HasPrefix(last, "Error:") {
				t.Fatalf("last log line %q", last)
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("Horizon task never finished")
}
