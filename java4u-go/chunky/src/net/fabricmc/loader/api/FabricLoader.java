package net.fabricmc.loader.api;

import java.io.File;
import java.nio.file.Path;
import java.util.Collection;
import java.util.List;
import java.util.Optional;
import java.util.function.Consumer;

import net.fabricmc.api.EnvType;
import net.fabricmc.loader.api.entrypoint.EntrypointContainer;

/**
 * The loader API that Fabric mods call. In Java4U it is implemented by Chunky, so mods
 * written for Fabric run unchanged.
 */
public interface FabricLoader {
	static FabricLoader getInstance() {
		return dev.java4u.chunky.ChunkyLoader.get();
	}

	<T> List<T> getEntrypoints(String key, Class<T> type);

	<T> List<EntrypointContainer<T>> getEntrypointContainers(String key, Class<T> type);

	<T> void invokeEntrypoints(String key, Class<T> type, Consumer<? super T> invoker);

	ObjectShare getObjectShare();

	MappingResolver getMappingResolver();

	Optional<ModContainer> getModContainer(String id);

	Collection<ModContainer> getAllMods();

	boolean isModLoaded(String id);

	boolean isDevelopmentEnvironment();

	EnvType getEnvironmentType();

	String getRawGameVersion();

	Object getGameInstance();

	Path getGameDir();

	@Deprecated
	File getGameDirectory();

	Path getConfigDir();

	@Deprecated
	File getConfigDirectory();

	String[] getLaunchArguments(boolean sanitize);
}
