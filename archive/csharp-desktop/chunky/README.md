# Chunky loader

Chunky is a separate Java runtime component used by Java4u. Version `0.1.0-alpha` begins a small Fabric metadata compatibility layer; it is not a Fabric Loader fork and does not bundle Fabric Loader.

## Build

Install a JDK 17 or newer and run `build.ps1`. The script compiles with Java 17 bytecode and writes `build/libs/chunky-loader.jar`. The Java4u desktop project runs this script during normal builds and copies the jar into its output package. GitHub Actions installs the build JDK before publishing.

## Implemented in alpha

- Read Fabric `fabric.mod.json` metadata in Java4u, validate mod ids and entrypoint shapes, detect duplicate ids, missing required mods and dependency cycles, and order dependencies before dependents.
- Launch the Minecraft main class through `chunky.bootstrap.ChunkyBootstrap` for Chunky profiles, with mod jars and the loader jar on that instance's classpath.
- Invoke default `main` and `client` entrypoints implementing `net.fabricmc.api.ModInitializer` or `ClientModInitializer`.
- Expose a small `FabricLoader` facade with `isModLoaded`, `getEnvironmentType`, `getGameDir`, and `getConfigDir`.
- Write loader startup and failure details to that instance's `.minecraft/logs/chunky-latest.log`.
- Block metadata that requests Mixins, access wideners, nested jars, language adapters, other entrypoint groups, or Fabric Loader services that the shim doesn't implement.

## Deliberate limits

This is an early experiment, not broad Fabric mod support. Initializers currently run before Minecraft's main method, so mods that need Fabric's normal game lifecycle or event hooks won't work. The loader has no Mixin transformer, mapping resolver, nested-jar support, Fabric API modules, full FabricLoader API, or dedicated-server mode. Java4u rejects metadata features it doesn't support, but metadata validation cannot guarantee arbitrary mod code is safe or compatible. Use a separate instance and back up worlds when experimenting.

The next loader milestones are a game lifecycle integration point, a compatible bytecode transformation layer, Fabric API modules, stronger version-range validation, and compatibility reports based on real mod launch outcomes. Compatibility remains incremental and must not be described as equivalent to Fabric until verified feature by feature.
