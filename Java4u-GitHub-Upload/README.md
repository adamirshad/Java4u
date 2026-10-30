# Java4u

Java4u is an open-source Minecraft Java launcher project. The launcher manages instances; **Chunky** is its independent, experimental loader with an initial, deliberately small Fabric metadata compatibility layer. The project is unofficial and is not affiliated with Mojang or Microsoft.

## Windows desktop app

The native app is in `desktop/`. It uses WPF and .NET 10; Chunky is a separate Java component in `chunky/`.

Build it on a .NET 10 Windows machine with a JDK 17 or newer available through `JAVA_HOME` or `PATH`:

```powershell
dotnet run --project desktop/Java4u.csproj
```

This slice can create, select, edit, and remove instance profiles with a Minecraft version, loader choice, memory limit, and optional per-instance `java.exe` or `javaw.exe` path. Leave the Java path blank to use Java on `PATH`. Check Java inspects the chosen runtime and compares its major version with the installed Minecraft version's requirement. The version field loads recent releases and snapshots from Mojang's official version manifest on demand, while remaining editable if offline. Install game files retrieves official version metadata, the client jar, Windows libraries and native jars, the asset index, and referenced assets into the selected instance. It records the Java major version required by that Minecraft version, checks HTTPS download hosts plus Mojang SHA-1 hashes and published sizes, reuses verified files, streams to temporary files, limits concurrent downloads to six, supports cancellation, and safely stages native extraction.

Each profile gets its own `.minecraft` game folder under `%LOCALAPPDATA%\Java4u\instances\`. Java4u prepares separate `mods`, `saves`, `config`, resource-pack, shader-pack, log, version, library, and asset folders for each instance. Use Open folder to reach the profile files. Installed status and required Java version are saved with each profile. The profile list is stored in `%LOCALAPPDATA%\Java4u\instances.json`, using a temporary file and atomic replacement when saving. Removing a profile does not delete its folder.

Java4u includes Microsoft device-code sign-in, verifies the account's Minecraft Java entitlement and profile, encrypts the saved session with Windows DPAPI for the current Windows user, refreshes sessions before launch, checks Java compatibility, and builds version-specific JVM/game arguments. If the right Java major version isn't installed, Java4u can download an Eclipse Temurin JRE, verify its SHA-256 and size against Adoptium metadata, and install it inside Java4u's local data folder. Configure Java4u's own Microsoft public-client app ID in Settings. Minecraft Java services may reject sign-in until Mojang approves the app registration.

The Modrinth browser searches for Fabric mods matching the selected Minecraft version, resolves required Modrinth dependencies, and verifies file hashes and sizes before placing JARs in that profile. Chunky alpha can invoke default `main` and `client` entrypoints for a narrow mod subset, with dependency ordering and a small `FabricLoader` facade. It blocks known unsupported features such as Mixins, access wideners, nested jars, custom entrypoint adapters, and declared Fabric Loader services. Entry points currently run before Minecraft's main method; normal Fabric lifecycle hooks, Mixin, Fabric API, and full Fabric Loader compatibility are still missing. Read [Chunky's compatibility limits](chunky/README.md) before trying mods. Search uses Modrinth's public API without a user token and identifies Java4u with a unique User-Agent.

See [Microsoft app setup](docs/MICROSOFT-APP-SETUP.md) before attempting sign-in. Microsoft/Mojang must approve Java4u's own registration before public Minecraft authentication can be considered ready.

## Build and package

Build locally with .NET 10 and JDK 17 or newer on Windows:

```powershell
dotnet build desktop/Java4u.csproj --configuration Release
```

The GitHub Actions workflow builds a self-contained Windows x64 package on pull requests and main branch pushes. Tag a version such as `v0.1.0` to create a GitHub release with the zipped launcher. The first published build is still an early preview; review the authentication and compatibility limitations before distributing it.

## Browser preview

Open `index.html` in a modern browser to see the original interactive layout concept. It uses mock data and does not connect to the desktop app.

## Product and engineering direction

- Keep the launcher (Java4u) and loader (Chunky) as separate components with independent release cycles.
- Build Chunky as its own loader. Treat Fabric compatibility as a defined adapter layer, not as renaming or bundling Fabric as the product.
- Target Fabric mod support through an explicit compatibility surface: `fabric.mod.json` metadata, dependency resolution, entrypoints, nested jars, environment rules, mappings, Mixin behavior, access wideners, and the Fabric API where required.
- Expect compatibility to be incremental. Some mods may depend on loader internals or API behavior Chunky does not implement yet; the launcher should report unsupported requirements before launch.
- Keep each instance isolated: game version, loader, mods, settings, and saves.
- Optimize for quick launch and low idle overhead: no background daemon, network work only when requested, verified metadata caching, bounded download concurrency, and direct JVM launch.
- Never distribute Minecraft game files. Download them from official sources and require a valid account/license for online play.
- Use Microsoft's sign-in flow; never ask for a player's Microsoft password.

## Build roadmap

1. **Launcher foundation (implemented):** local profiles, isolated game folders, memory settings, and official version selection.
2. **Game installation (implemented):** verified version metadata, client, libraries, assets, and native extraction.
3. **Java setup (implemented):** Java inspection, Temurin runtime installation, and version-specific JVM arguments.
4. **Account flow (implemented, registration dependent):** Microsoft sign-in, entitlement checks, token refresh, and Windows user encryption. Public Minecraft authentication still depends on approval for Java4u's own Microsoft app registration.
5. **Vanilla launch (implemented, requires a valid account):** authenticated launch of installed Vanilla profiles.
6. **Mod management (implemented):** Modrinth search, required dependency resolution, and verified profile mod installation.
7. **Chunky alpha (implemented, experimental):** separate Java runtime jar, Fabric `main`/`client` entrypoint bridge, basic dependency ordering, limited FabricLoader facade, and unsupported-feature checks.
8. **Fabric compatibility (in progress):** game lifecycle hooks, Mixin/mappings, Fabric API modules, nested jars, and broader FabricLoader services remain to be implemented and verified.

## Repository hygiene

The .NET project has no third-party package dependencies. Build output and local SDK setup files are ignored by `.gitignore`.
