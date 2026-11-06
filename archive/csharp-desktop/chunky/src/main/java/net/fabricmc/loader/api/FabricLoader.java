package net.fabricmc.loader.api;

import chunky.bootstrap.ChunkyLoaderContext;
import net.fabricmc.api.EnvType;

import java.nio.file.Path;

/** Minimal read-only FabricLoader facade. Calls outside this surface fail at compile/link time. */
public final class FabricLoader {
    private static final FabricLoader INSTANCE = new FabricLoader();
    private FabricLoader() { }

    public static FabricLoader getInstance() { return INSTANCE; }
    public boolean isModLoaded(String id) { return ChunkyLoaderContext.isModLoaded(id); }
    public EnvType getEnvironmentType() { return EnvType.CLIENT; }
    public Path getGameDir() { return Path.of(System.getProperty("user.dir")).toAbsolutePath(); }
    public Path getConfigDir() { return getGameDir().resolve("config"); }
}
