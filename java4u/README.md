# java4u

A Minecraft launcher written in Rust. Cargo workspace:

- `crates/java4u-core`: versions, parallel hash-verified downloads, libraries/natives, assets, Java detection, instances, launch command builder.
- `crates/java4u-cli`: `java4u` command-line front-end.
- `crates/java4u-gui`: `java4u-gui` desktop app (egui) with custom wallpapers.

## Get an executable

Easiest, no Rust needed: push this folder to a GitHub repo. The included workflow builds Windows,
macOS and Linux executables; download them from the run's **Artifacts** (Actions tab).

On your own machine: install Rust from https://rustup.rs, then run `build.bat` (Windows) or
`./build.sh` (macOS/Linux). The programs end up in `target/release/`:
`java4u` (command line) and `java4u-gui` (desktop app).

## Build

    cargo build --release
    cargo test

## Desktop app

    cargo run -p java4u-gui --release

Tabs: Play, Instances, Accounts, Appearance. On Linux you need the GTK3 dev package for the file
picker (`libgtk-3-dev`) plus the usual egui windowing libraries (libxkbcommon, libwayland, libx11).

**Wallpapers:** Appearance tab, "Choose image..." or drag an image file onto the window
(png, jpg, webp, gif, bmp). Images are validated, shrunk to at most 2560x1440 and stored as JPEG in the
data dir, so the original can be moved or deleted. A darkening slider keeps text readable.

## Use

    java4u list                          # releases (add --snapshots / --all)
    java4u install latest                # download client, libraries, assets
    java4u instance create main 1.21.1
    java4u account login                 # Microsoft sign-in (needs --client-id the first time)
    java4u launch main                   # plays as your saved account
    java4u launch main --offline Steve   # offline, dev/testing only
    java4u launch main --dry-run         # print the java command
    java4u instance set main --auto-memory --profile performance
    java4u install 1.21.1 --verify       # re-check every file's SHA1 (repair)

Data lives in your platform data dir; override with `--data-dir` or `JAVA4U_HOME`.
Java is handled for you: the exact runtime Mojang ships for each Minecraft version is downloaded on first
launch into `runtimes/` (delete a folder there to force an update). If Mojang publishes none for your
platform (e.g. Linux ARM), a suitable system Java from JAVA_HOME or PATH is used instead.

## Optimisation

- **Memory:** new instances get a heap sized to your RAM (never above half of it).
- **JVM profiles:** `off`, `balanced` (default, G1 with short pauses) and `performance` (tuned G1 flags).
  Your own `-Xms` or `-XX:+Use*GC` arguments are respected and override the profile.
- **Fast launches:** after one fully verified install, later launches only compare file sizes instead of
  re-hashing every library and asset. Downloads are parallel, and only missing files are fetched.

## Microsoft sign-in setup

1. In the Azure portal, register an app: supported account types = *Personal Microsoft accounts only*,
   then under Authentication enable **Allow public client flows**. Copy the Application (client) ID.
2. Request Minecraft API access for that app (Mojang's app registration form, https://aka.ms/AppRegistrationForm).
   Until it is approved, the last step of login fails with a 403.
3. `java4u account login --client-id <ID>`. The id is remembered; refresh tokens keep you signed in.

Accounts are stored in `accounts.json` in the data dir (mode 0600 on Unix). Moving tokens into the OS
keychain (`keyring` crate) is a planned hardening step.

## Status

Done: version manifest, downloader (SHA1, resume-safe .part files, retries), rule evaluation,
libraries and legacy natives, assets (incl. virtual/map_to_resources), instances, launch.

Also done: Microsoft sign-in (device code, Xbox, XSTS, Minecraft services) with token refresh.

Also done: automatic Java runtime download, JVM profiles, desktop app with wallpapers.

Next: Fabric/Quilt (`inheritsFrom`),
Modrinth, Forge/NeoForge, modpacks.
