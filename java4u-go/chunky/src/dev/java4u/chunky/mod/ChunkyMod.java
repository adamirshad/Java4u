package dev.java4u.chunky.mod;

import java.io.IOException;
import java.net.URI;
import java.nio.file.FileSystem;
import java.nio.file.FileSystemAlreadyExistsException;
import java.nio.file.FileSystems;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.Optional;

import net.fabricmc.loader.api.ModContainer;
import net.fabricmc.loader.api.metadata.ModMetadata;
import net.fabricmc.loader.api.metadata.ModOrigin;

/** A loaded mod: its metadata plus the jar (or nothing, for built-ins) it came from. */
public final class ChunkyMod implements ModContainer {
	public final ModMeta meta;
	/** The jar on disk, or null for built-in mods without files. */
	public final Path jar;
	private final ChunkyMod parent;
	private final String subLocation;
	private final List<ChunkyMod> children = new ArrayList<>();
	private volatile List<Path> roots;

	public ChunkyMod(ModMeta meta, Path jar, ChunkyMod parent, String subLocation) {
		this.meta = meta;
		this.jar = jar;
		this.parent = parent;
		this.subLocation = subLocation;
		if (parent != null) parent.children.add(this);
	}

	public String id() {
		return meta.getId();
	}

	@Override
	public ModMetadata getMetadata() {
		return meta;
	}

	/** Opens the jar as a file system the first time a mod asks for its files. */
	@Override
	public List<Path> getRootPaths() {
		List<Path> r = roots;
		if (r != null) return r;
		synchronized (this) {
			if (roots != null) return roots;
			if (jar == null) {
				roots = Collections.emptyList();
			} else if (Files.isDirectory(jar)) {
				roots = Collections.singletonList(jar);
			} else {
				roots = Collections.singletonList(openZip(jar).getPath("/"));
			}
			return roots;
		}
	}

	static FileSystem openZip(Path jar) {
		URI uri = URI.create("jar:" + jar.toUri());
		try {
			return FileSystems.newFileSystem(uri, Map.of());
		} catch (FileSystemAlreadyExistsException e) {
			return FileSystems.getFileSystem(uri);
		} catch (IOException e) {
			throw new java.io.UncheckedIOException("can't open " + jar, e);
		}
	}

	@Override
	public ModOrigin getOrigin() {
		ModOrigin.Kind kind = parent != null ? ModOrigin.Kind.NESTED : jar != null ? ModOrigin.Kind.PATH : ModOrigin.Kind.UNKNOWN;
		return new ModOrigin() {
			@Override
			public Kind getKind() {
				return kind;
			}

			@Override
			public List<Path> getPaths() {
				if (kind != Kind.PATH) throw new UnsupportedOperationException("origin is " + kind);
				return Collections.singletonList(jar);
			}

			@Override
			public String getParentModId() {
				if (kind != Kind.NESTED) throw new UnsupportedOperationException("origin is " + kind);
				return parent.id();
			}

			@Override
			public String getParentSubLocation() {
				if (kind != Kind.NESTED) throw new UnsupportedOperationException("origin is " + kind);
				return subLocation;
			}

			@Override
			public String toString() {
				return kind == Kind.NESTED ? parent.id() + "/" + subLocation : String.valueOf(jar);
			}
		};
	}

	@Override
	public Optional<ModContainer> getContainingMod() {
		return Optional.ofNullable(parent);
	}

	@Override
	public Collection<ModContainer> getContainedMods() {
		return Collections.unmodifiableList(children);
	}

	@Override
	@Deprecated
	public Path getRootPath() {
		List<Path> r = getRootPaths();
		if (r.isEmpty()) throw new UnsupportedOperationException(id() + " has no files");
		return r.get(0);
	}

	@Override
	@Deprecated
	public Path getPath(String file) {
		return findPath(file).orElseGet(() -> getRootPath().resolve(file));
	}

	@Override
	public String toString() {
		return meta.toString();
	}
}
