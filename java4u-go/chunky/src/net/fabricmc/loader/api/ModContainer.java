package net.fabricmc.loader.api;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Collection;
import java.util.List;
import java.util.Optional;

import net.fabricmc.loader.api.metadata.ModMetadata;
import net.fabricmc.loader.api.metadata.ModOrigin;

/** A loaded mod: its metadata and the files it was loaded from. */
public interface ModContainer {
	ModMetadata getMetadata();

	List<Path> getRootPaths();

	default Optional<Path> findPath(String file) {
		for (Path root : getRootPaths()) {
			Path p = root.resolve(file.replace("/", root.getFileSystem().getSeparator()));
			if (Files.exists(p)) return Optional.of(p);
		}
		return Optional.empty();
	}

	ModOrigin getOrigin();

	Optional<ModContainer> getContainingMod();

	Collection<ModContainer> getContainedMods();

	@Deprecated
	default Path getRoot() {
		return getRootPath();
	}

	@Deprecated
	Path getRootPath();

	@Deprecated
	Path getPath(String file);
}
