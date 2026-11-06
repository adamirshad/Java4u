// Command java4u-gui is the Java4U desktop app. It starts the launcher on 127.0.0.1 and
// opens it in a browser app window (Edge or Chrome when available, otherwise the
// default browser). It exits when you press Quit or close the window.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/adamirshad/Java4u/java4u-go/internal/core"
	"github.com/adamirshad/Java4u/java4u-go/internal/gui"
)

func main() {
	noWindow := flag.Bool("no-window", false, "don't open a window; just print the address")
	addr := flag.String("addr", "127.0.0.1:0", "address to listen on (keep it on 127.0.0.1)")
	dataDir := flag.String("data-dir", "", "where Java4U stores everything (default: JAVA4U_HOME or the platform data folder)")
	flag.Parse()

	dirs, err := core.DefaultDirs()
	if *dataDir != "" {
		dirs, err = core.NewDirs(*dataDir), nil
	}
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(dirs.Root, 0o755); err != nil {
		fatal(err)
	}
	// The windowed build has no console, so keep a log file next to the data.
	if f, err := os.OpenFile(filepath.Join(dirs.Root, "java4u.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
		log.SetOutput(f)
		defer f.Close()
	}

	srv, url, err := gui.Start(dirs, *addr)
	if err != nil {
		fatal(err)
	}
	log.Printf("Java4U %s running; data in %s", core.Version, dirs.Root)
	fmt.Println("Java4U is running at", url)
	if !*noWindow {
		if err := openWindow(url); err != nil {
			log.Printf("could not open a window: %v", err)
			fmt.Println("Open the address above in your browser.")
		}
	}
	<-srv.Done()
}

func fatal(err error) {
	log.Print(err)
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

// appBrowsers lists Chromium-based browsers that support --app=URL (a window
// without tabs or an address bar), most likely first.
func appBrowsers() []string {
	switch runtime.GOOS {
	case "windows":
		var out []string
		for _, base := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LocalAppData")} {
			if base == "" {
				continue
			}
			out = append(out,
				filepath.Join(base, `Microsoft\Edge\Application\msedge.exe`),
				filepath.Join(base, `Google\Chrome\Application\chrome.exe`))
		}
		return out
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	default:
		var out []string
		for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge"} {
			if p, err := exec.LookPath(name); err == nil {
				out = append(out, p)
			}
		}
		return out
	}
}

func openWindow(url string) error {
	for _, b := range appBrowsers() {
		if _, err := os.Stat(b); err != nil {
			continue
		}
		cmd := exec.Command(b, "--app="+url, "--window-size=1360,860", "--new-window")
		if err := cmd.Start(); err == nil {
			go cmd.Wait()
			return nil
		}
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
