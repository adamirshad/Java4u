# Java4U

Java4U is an unofficial, open-source Minecraft: Java Edition launcher written in Go, with a goose
for a mascot. It replaces the earlier Rust version in `../java4u`. It is not affiliated with
Mojang or Microsoft.

- `internal/core`: versions, parallel hash-verified downloads, libraries/natives, assets, Java
  runtimes, Microsoft sign-in, instances, launch command builder.
- `cmd/java4u`: `java4u` command-line tool.
- `cmd/java4u-gui`: `java4u-gui` desktop app.
- `chunky/`: **Chunky**, Java4U's own mod loader. It's written in Java and runs Fabric mods
  (see [chunky/README.md](chunky/README.md)).

Java4U's only dependency outside Go's standard library is `golang.org/x/image` (for webp and bmp
wallpapers). It needs no C compiler, so it builds for Windows, macOS and Linux from any machine.

## Get an executable

Push the repository to GitHub. The `Java4U` workflow builds Windows, macOS and Linux executables,
which you can download from the run's **Artifacts** (Actions tab).

On your own machine, install Go 1.25 or newer from https://go.dev/dl, plus a JDK 21 or newer to
build Chunky. Then run this from this folder:

    go run ./tools/buildchunky                             # builds Chunky, which Java4U embeds
    go build ./cmd/java4u
    go build -ldflags "-H=windowsgui" ./cmd/java4u-gui     # Windows: no console window
    go build ./cmd/java4u-gui                              # macOS / Linux
    go test ./...

## Desktop app

    go run ./cmd/java4u-gui

The sidebar has Home, Versions, Mods, Modpacks, Account, Settings and About. The Home page has:
- A banner with a big **Launch Game** button. The banner shows a pixel-art scene or your own
  picture, and rotates through the latest Minecraft news.
- Quick pickers for the version, performance profile and RAM.
- Live panels with popular mods and modpacks from Modrinth and Mojang's latest Java news.

The top-right corner shows your Minecraft head and lets you switch accounts. The interface is a local web page
served on 127.0.0.1 and protected by a one-time token. It opens in an Edge or Chrome app window
(no tabs or address bar) when one is installed, otherwise in your default browser. Java4U exits
when you press **Quit**, or a few minutes after you close the window if nothing is running.
Minecraft keeps running if you close Java4U. `--no-window` only prints the address.

**Banner image:** On the Settings page, use "Choose image..." or drag an image onto the window
(png, jpg, webp, gif or bmp). Images are checked, shrunk to at most 2560x1440 and stored as JPEG in
the data folder, so you can move or delete the original. A darkening slider keeps text readable.

## Use

    java4u list                          # releases (add --snapshots / --all)
    java4u install latest                # download client, libraries, assets
    java4u instance create main 1.21.1
    java4u account login                 # Microsoft sign-in
    java4u launch main                   # plays as your saved account
    java4u launch main --offline Steve   # offline, dev/testing only
    java4u launch main --dry-run         # print the java command (access token hidden)
    java4u instance set main --auto-memory --profile performance
    java4u install 1.21.1 --verify       # re-check every file's SHA1 (repair)

## Mods

Mods run on **Chunky**, Java4U's own lightweight mod loader, on Minecraft 26.1 and newer. Chunky
runs Fabric mods as they are, so the whole Fabric mod library on Modrinth works with it.

In the app, open **Mods** and press **Install** on any mod. Java4U fetches the Fabric build for
your version's Minecraft release, checks its hash, installs anything it requires, and switches the
version to Chunky. Each version's page lists its mods, where you can turn them on or off or
remove them. From the command line:

    java4u mods install main sodium      # also switches "main" to Chunky
    java4u mods list main
    java4u mods disable main sodium-fabric-0.9.2+mc26.3.jar
    java4u instance set main --loader vanilla   # back to plain Minecraft (mods stay on disk)

## Modpacks

Open **Modpacks** in the app and press **Install** on any pack. The list shows Fabric packs that
have a build for a version Chunky runs. Java4U does the rest:

- Downloads the pack and creates a new version named after it, set to Chunky.
- Fetches every file the pack lists. Each download is checked against its SHA-1 and must come
  from Modrinth, GitHub or GitLab.
- Copies in the pack's configs. Paths that would escape the version's folder are refused.

If anything fails, the half-installed version is removed. From the command line:

    java4u modpack install fabulously-optimized

## Servers

The **Servers** page runs your own dedicated server on this PC, with mods through Chunky:

- Create a server and tick the box to agree to Minecraft's EULA (Java4U never agrees for you).
- Press **Start server**, follow the console, and type commands such as `op YourName`.
- Search for mods and install them. Client-only mods such as Sodium are refused, and
  client-only parts of other mods are skipped.
- The page shows the address friends use: `localhost` on this PC, or your home-network IP.

From the command line:

    java4u server create friends 26.3
    java4u server mods install friends fabric-api
    java4u server run friends --accept-eula   # only if you agree to https://aka.ms/MinecraftEULA

Data lives in `%APPDATA%\Java4U`, `~/Library/Application Support/Java4U` or `~/.local/share/java4u`.
Override it with `--data-dir` or `JAVA4U_HOME`. Java4U uses the same file formats as the Rust
java4u launcher, so you can copy a java4u data folder (instances, accounts, settings) into
Java4U's folder.

Java is handled for you. On first launch Java4U downloads the exact runtime Mojang ships for that
Minecraft version into `runtimes/`; delete a folder there to force an update. If Mojang publishes
no runtime for your platform (for example Linux ARM), Java4U uses a suitable system Java from
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

Java4U ships with its own Azure app, which Mojang has approved for the Minecraft API, so sign-in
works out of the box. Run `java4u account login`, or click **Sign in with Microsoft** on the
Accounts tab. Then enter the code Java4U shows at Microsoft's page. Refresh tokens keep you
signed in.

To use your own Azure app instead (for a fork, for example):

1. In the Azure portal, register an app with supported account types set to *Personal Microsoft
   accounts only*. Under Authentication, enable **Allow public client flows**. Copy the Application
   (client) ID.
2. Request Minecraft API access for that app using Mojang's app registration form
   (https://aka.ms/AppRegistrationForm). Until it is approved, the last step of sign-in fails
   with a 403.
3. Run `java4u account login --client-id <ID>`, set `JAVA4U_CLIENT_ID`, or paste the ID under
   "Advanced" on the Accounts tab. Java4U remembers the ID used for each sign-in.

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
- Chunky mod loader, running Fabric mods on 26.x clients and dedicated servers. Tested with
  Fabric API, Sodium, Mod Menu and the 47-mod Fabulously Optimized pack (143 mods counting
  nested ones).
- One-click mod and modpack installs from Modrinth, with dependencies.
- Dedicated servers with a console, commands and server mods.

Next: remapping so Chunky can support versions before 26.1, and modpack updates.
