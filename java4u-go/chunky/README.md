# Chunky

Chunky is Java4U's own mod loader. It runs **Fabric mods unchanged** on Minecraft 26.1 and
newer. It does this without Fabric Loader: it provides the Fabric Loader API itself,
written from scratch.

## Why Chunky is light

- **No remapping.** Minecraft 26.x ships with real class names, so Chunky never translates
  names between mapping sets. Loaders that remap need extra startup time and memory for
  mapping tables. Chunky has none.
- **Lazy patching.** Classes are patched only the first time the game uses them. A class
  that no mod touches is passed through without being parsed.
- **No caches in memory.** Jars stay open, and class bytes are read when needed and
  dropped once the class is defined.
- **Small.** The whole loader is about 115 KB. Its only libraries are Mixin, MixinExtras
  and ASM, which Fabric mods need anyway.

Measured on Minecraft 26.3 with Fabric API (44 modules), Sodium and Mod Menu: Chunky was
ready in under 1 second with 63 Mixin configs, and all mods initialized in about 320 ms.

## What it supports

- `fabric.mod.json` (schema 1) and Chunky's own `chunky.mod.json`, which uses the same
  layout.
- Jars nested inside mods (`jars`), which are extracted once into `.chunky/nested`, named by
  hash.
- Dependency checks (`depends`, `breaks`, `conflicts`, `recommends`) with full version
  ranges (`>=`, `<`, `~`, `^`, `1.21.x`) and clear error messages.
- Mixin configs, including MixinExtras, with per-mod compatibility levels.
- Access wideners (v1 and v2) and class tweakers, including `inject-interface`.
- The `main`, `client`, `preLaunch` and custom entrypoints, plus mods' own language
  adapters (for example Kotlin).
- The whole public `net.fabricmc.loader.api` and `net.fabricmc.api` surface:
  `FabricLoader`, `ModContainer`, metadata, versions, `ObjectShare`, and
  `MappingResolver`, which maps every name to itself on 26.x.

## Limits

- Client only for now. Dedicated servers are next.
- Minecraft 26.1 and newer only. Older, obfuscated versions would need a remapping layer.
- Mods that reach into Fabric Loader's internal (`impl`) classes won't work.
- `@Environment` stripping isn't done. Server-only code in client mods is simply never
  loaded.

## How it starts

Java4U launches `dev.java4u.chunky.Main`, with Chunky, Mixin and ASM on the JVM classpath.
Chunky then does the following:

1. Finds mods in the instance's `mods` folder and resolves them.
2. Loads the game, its libraries and every mod in one class loader.
3. Applies access wideners and Mixin.
4. Runs `preLaunch`, then starts Minecraft.
5. Runs `main` and `client` entrypoints from inside Minecraft's constructor, once the game
   and player exist.

The log is written to `logs/chunky-latest.log` in the instance folder.

## Building

From `java4u-go`:

    go run ./tools/buildchunky

This needs JDK 21 or newer. A `java` that includes the `jdk.compiler` module also works;
pass it with `-java`. The tool downloads the pinned libraries, compiles Chunky, and writes
`internal/chunky/dist/chunky.jar`, which Java4U embeds.
