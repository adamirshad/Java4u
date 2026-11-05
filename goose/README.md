# Goose

Goose is an unofficial, open-source Minecraft: Java Edition launcher written in Go. It is the Go
rewrite of Java4u. It is not affiliated with Mojang or Microsoft.

- `internal/core`: versions, parallel hash-verified downloads, libraries/natives, assets, Java
  runtimes, Microsoft sign-in, instances, launch command builder.
- `cmd/goose`: `goose` command-line tool.
- `cmd/goose-gui`: `goose-gui` desktop app with custom wallpapers.

Goose's only dependency outside Go's standard library is `golang.org/x/image` (for webp and bmp
wallpapers). It needs no C compiler, so it builds for Windows, macOS and Linux from any machine.

## Get an executable

Push the repository to GitHub. The `Goose` workflow builds Windows, macOS and Linux executables,
which you can download from the run's **Artifacts** (Actions tab).

On your own machine, install Go 1.25 or newer from https://go.dev/dl, then run this from this folder:

    go build ./cmd/goose
    go build -ldflags "-H=windowsgui" ./cmd/goose-gui     # Windows: no console window
    go build ./cmd/goose-gui                              # macOS / Linux
    go test ./...

## Desktop app

    go run ./cmd/goose-gui

The sidebar has Home, Versions, Mods, Modpacks, Account, Settings and About. The Home page has:
- A banner with a big **Launch Game** button. The banner shows a pixel-art scene or your own
  picture, and rotates through the latest Minecraft news.
- Quick pickers for the version, performance profile and RAM.
- Live panels with popular mods and modpacks from Modrinth and Mojang's latest Java news.

The top-right corner shows your Minecraft head and lets you switch accounts. The interface is a local web page
served on 127.0.0.1 and protected by a one-time token. It opens in an Edge or Chrome app window
(no tabs or address bar) when one is installed, otherwise in your default browser. Goose exits
when you press **Quit**, or a few minutes after you close the window if nothing is running.
Minecraft keeps running if you close Goose. `--no-window` only prints the address.

**Banner image:** On the Settings page, use "Choose image..." or drag an image onto the window
(png, jpg, webp, gif or bmp). Images are checked, shrunk to at most 2560x1440 and stored as JPEG in
the data folder, so you can move or delete the original. A darkening slider keeps text readable.

## Use

    goose list                          # releases (add --snapshots / --all)
    goose install latest                # download client, libraries, assets
    goose instance create main 1.21.1
    goose account login                 # Microsoft sign-in (needs --client-id the first time)
    goose launch main                   # plays as your saved account
    goose launch main --offline Steve   # offline, dev/testing only
    goose launch main --dry-run         # print the java command (access token hidden)
    goose instance set main --auto-memory --profile performance
    goose install 1.21.1 --verify       # re-check every file's SHA1 (repair)

Data lives in `%APPDATA%\Goose`, `~/Library/Application Support/Goose` or `~/.local/share/goose`.
Override it with `--data-dir` or `GOOSE_HOME`. Goose uses the same file formats as the Rust
java4u launcher, so you can copy a java4u data folder (instances, accounts, settings) into
Goose's folder.

Java is handled for you. On first launch Goose downloads the exact runtime Mojang ships for that
Minecraft version into `runtimes/`; delete a folder there to force an update. If Mojang publishes
no runtime for your platform (for example Linux ARM), Goose uses a suitable system Java from
JAVA_HOME or PATH.

## Optimisation

- **Memory:** new instances get a heap sized to your RAM, never more than half of it.
- **JVM profiles:** `off`, `balanced` (default; G1 with short pauses) and `performance` (tuned G1
  flags). Your own `-Xms` or `-XX:+Use*GC` arguments are respected and override the profile.
- **Fast launches:** after one fully verified install, later launches compare file sizes instead
  of re-hashing every library and asset. Downloads run 16 at a time, and only missing files are
  fetched.
- **Account first:** launching checks your sign-in before downloading anything, so a missing
  account fails immediately.

## Microsoft sign-in

Goose ships with its own Azure app, which Mojang has approved for the Minecraft API, so sign-in
works out of the box. Run `goose account login`, or click **Sign in with Microsoft** on the
Accounts tab. Then enter the code Goose shows at Microsoft's page. Refresh tokens keep you
signed in.

To use your own Azure app instead (for a fork, for example):

1. In the Azure portal, register an app with supported account types set to *Personal Microsoft
   accounts only*. Under Authentication, enable **Allow public client flows**. Copy the Application
   (client) ID.
2. Request Minecraft API access for that app using Mojang's app registration form
   (https://aka.ms/AppRegistrationForm). Until it is approved, the last step of sign-in fails
   with a 403.
3. Run `goose account login --client-id <ID>`, set `GOOSE_CLIENT_ID`, or paste the ID under
   "Advanced" on the Accounts tab. Goose remembers the ID used for each sign-in.

Accounts are stored in `accounts.json` in the data folder (mode 0600 on Unix). Moving tokens into
the OS keychain is a planned hardening step.

## Status

Done:
- Version manifest and downloader (SHA1 checks, atomic `.part` files, retries).
- Rule evaluation, libraries and legacy natives.
- Assets, including virtual and map_to_resources.
- Instances and launching.
- Microsoft sign-in (device code, Xbox, XSTS, Minecraft services) with token refresh.
- Automatic Java runtime download and JVM profiles.
- Desktop app with wallpapers.

Next: Fabric/Quilt (`inheritsFrom`), Modrinth, Forge/NeoForge, modpacks.
