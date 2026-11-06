# Java4U

Java4U is a free, open-source Minecraft: Java Edition launcher for Windows, macOS and Linux,
with a goose for a mascot. It has its own mod loader, **Chunky**, which runs Fabric mods.
This project is not affiliated with Mojang or Microsoft.

## What it does

- **Play:** Microsoft sign-in, downloads verified against Mojang's hashes, the right Java
  runtime fetched automatically, and separate versions (instances) with their own worlds,
  memory and performance settings.
- **Mods:** one-click installs from Modrinth, including their dependencies. Mods run on Chunky,
  Java4U's lightweight loader for Minecraft 26.1 and newer, which runs Fabric mods unchanged
  with no remapping.
- **Modpacks:** install a Modrinth pack as a new ready-to-play version. Turn whole packs or
  single mods on and off.
- **Servers:** run a dedicated server on your PC, with a console, commands and server mods.
- **Desktop app and CLI:** a green, Minecraft-styled app (`java4u-gui`) and a command-line tool
  (`java4u`).

## Where things are

- [`java4u-go/`](java4u-go): the launcher, written in Go, including the desktop app and CLI.
  See its [README](java4u-go/README.md) for building and usage.
- [`java4u-go/chunky/`](java4u-go/chunky): Chunky, the mod loader, written in Java. See its
  [README](java4u-go/chunky/README.md).
- [`archive/`](archive): earlier C# and Rust versions, kept for reference.

## Get it

Every push to `main` builds Windows, macOS and Linux executables. Download them from the
latest run of the **Java4U** workflow on the Actions tab (under **Artifacts**). Then run
`java4u-gui`.

To build it yourself, you need Go 1.25+ and a JDK 21+. From `java4u-go`:

    go run ./tools/buildchunky
    go build ./cmd/java4u-gui

## Project principles

- Keep Java4U and Chunky as separate components.
- Never bundle Minecraft game files. Download them from official sources and verify them.
- Require a legitimate Minecraft license for online play.
- Use Microsoft's sign-in flow and never collect a player's Microsoft password.
- Players accept Minecraft's EULA themselves; Java4U never accepts it for them.
- Keep network work on demand, verify every download, limit concurrent downloads, and launch
  the game JVM directly.

The repository uses the GPL-3.0 license.
