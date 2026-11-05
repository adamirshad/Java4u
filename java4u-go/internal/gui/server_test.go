package gui

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"github.com/adamirshad/Java4u/java4u-go/internal/core"
)

func start(t *testing.T) (*Server, string, *http.Client) {
	t.Helper()
	dirs := core.NewDirs(t.TempDir())
	s, url, err := Start(dirs, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.cancel)
	jar, _ := cookiejar.New(nil)
	return s, url, &http.Client{Jar: jar}
}

func postJSON(t *testing.T, c *http.Client, url string, body any) *http.Response {
	t.Helper()
	data, _ := json.Marshal(body)
	resp, err := c.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestAuthFlow(t *testing.T) {
	s, url, c := start(t)
	base := "http://" + s.host

	resp, _ := c.Get(base + "/api/data")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no cookie: got %d", resp.StatusCode)
	}
	resp, _ = c.Get(base + "/?t=wrong")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong token: got %d", resp.StatusCode)
	}

	resp, err := c.Get(url) // sets the cookie and redirects to /
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("login: %v %v", resp.Status, err)
	}
	resp, _ = c.Get(base + "/api/data")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("with cookie: got %d", resp.StatusCode)
	}

	// Foreign Host header (DNS rebinding) is refused.
	req, _ := http.NewRequest("GET", base+"/api/data", nil)
	req.Host = "evil.example:80"
	resp, _ = c.Do(req)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("bad host: got %d", resp.StatusCode)
	}

	// Cross-origin POST is refused even with the cookie.
	req, _ = http.NewRequest("POST", base+"/api/instances", strings.NewReader(`{"name":"x","version":"1"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.example")
	resp, _ = c.Do(req)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross origin: got %d", resp.StatusCode)
	}
}

func TestInstancesAndSettings(t *testing.T) {
	s, url, c := start(t)
	base := "http://" + s.host
	c.Get(url)

	resp := postJSON(t, c, base+"/api/instances", map[string]string{"name": "main", "version": "1.21.1"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	resp = postJSON(t, c, base+"/api/instances", map[string]string{"name": "../bad", "version": "1.21.1"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad name: %d", resp.StatusCode)
	}
	resp = postJSON(t, c, base+"/api/instances/save", map[string]any{
		"name": "main", "memory_mb": 3072, "profile": "performance", "jvm_args": "-Dfoo=1  -Dbar=2", "java_path": "",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save: %d", resp.StatusCode)
	}
	inst, err := core.LoadInstance(s.dirs, "main")
	if err != nil || inst.MemoryMB != 3072 || inst.Profile != core.ProfilePerformance || len(inst.JVMArgs) != 2 {
		t.Fatalf("saved instance: %+v %v", inst, err)
	}

	resp = postJSON(t, c, base+"/api/settings", map[string]any{"wallpaper_dim": 5.0, "show_snapshots": true})
	var st core.Settings
	json.NewDecoder(resp.Body).Decode(&st)
	if st.WallpaperDim != 0.9 || !st.ShowSnapshots {
		t.Fatalf("settings: %+v", st)
	}
	resp = postJSON(t, c, base+"/api/settings", map[string]any{"wallpaper": "../../x.jpg"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad wallpaper name: %d", resp.StatusCode)
	}

	// Offline play needs a valid username.
	resp = postJSON(t, c, base+"/api/play", map[string]any{"instance": "main", "offline": true, "offline_username": "x"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad offline name: %d", resp.StatusCode)
	}

	resp = postJSON(t, c, base+"/api/instances/delete", map[string]string{"name": "main"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
}

func TestContentInputChecks(t *testing.T) {
	s, url, c := start(t)
	base := "http://" + s.host
	c.Get(url)

	resp, _ := c.Get(base + "/api/browse?type=plugin")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad browse type: %d", resp.StatusCode)
	}
	resp, _ = c.Get(base + "/api/avatar/not-a-uuid")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("bad uuid: %d", resp.StatusCode)
	}

	// Only images from the hosts the page's CSP allows get through.
	if imageURL("/v2/images/x.jpg") != "https://launchercontent.mojang.com/v2/images/x.jpg" ||
		imageURL("//evil.example/x.jpg") != "" || imageURL("https://evil.example/x.jpg") != "" {
		t.Fatal("imageURL")
	}
	if modrinthImage("https://cdn.modrinth.com/data/a.png") == "" || modrinthImage("https://evil.example/a.png") != "" {
		t.Fatal("modrinthImage")
	}
}

func TestWallpaperUpload(t *testing.T) {
	s, url, c := start(t)
	base := "http://" + s.host
	c.Get(url)

	var img bytes.Buffer
	png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 10, 10)))
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "sky.png")
	fw.Write(img.Bytes())
	mw.Close()
	resp, err := c.Post(base+"/api/wallpapers", mw.FormDataContentType(), &body)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("upload: %v %v", resp.Status, err)
	}
	resp, _ = c.Get(base + "/wallpapers/sky.jpg")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("serve: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if s.settings.Wallpaper != "sky.jpg" {
		t.Fatal("uploaded wallpaper not selected")
	}
}
