# Java4u

Java4u is an unofficial, open-source Minecraft Java launcher for Windows. It manages isolated game profiles. Chunky is its planned independent mod loader, with Fabric mod compatibility as a long-term goal. This project is not affiliated with Mojang or Microsoft.

## Current status

- The WPF desktop app targets .NET 10 and has no third-party package dependencies.
- Create, select, edit, and remove profiles with a Minecraft version, loader choice, and memory limit.
- Each profile gets its own folder under %LOCALAPPDATA%\Java4u\instances\. The profile list is stored in %LOCALAPPDATA%\Java4u\instances.json.
- Removing a profile leaves its folder intact.
- The browser preview is a separate layout concept and does not connect to the desktop app.
- Minecraft downloads, Microsoft sign-in, Java runtime management, game launching, and the Chunky runtime are not implemented yet.

## Run the Windows app

Requires .NET 10 on Windows. From the repository root:

```powershell
dotnet run --project desktop/Java4u.csproj
```

## Chunky and Fabric compatibility

Chunky is intended to be its own loader, separate from Fabric. Compatibility with Fabric mods must be implemented in stages, including Fabric mod metadata, dependencies, entrypoints, nested jars, mappings, Mixin behavior, access wideners, and Fabric API support where required. Some mods may remain incompatible; Java4u should identify missing requirements before launch.

## Project principles

- Keep Java4u and Chunky as separate components.
- Never bundle Minecraft game files. Retrieve game files from official sources and verify them.
- Require a legitimate Minecraft license for online play.
- Use Microsoft's sign-in flow; never collect a player's Microsoft password.
- Keep network work on demand, cache verified metadata, bound download concurrency, and launch the game JVM directly.

## Build roadmap

1. Instance library, local profile storage, isolated folders, and memory settings.
2. Official version selection and editable per-instance settings.
3. Verified game file downloads with resumable transfers.
4. Microsoft sign-in, entitlement checks, refresh, and secure token storage.
5. Java runtime selection and JVM argument generation.
6. Chunky loader specification and a native-mod MVP.
7. Incremental Fabric compatibility and compatibility reporting.

See CONTRIBUTING.md for contribution and build guidance. The repository uses the GPL-3.0 license.
