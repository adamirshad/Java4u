package net.fabricmc.loader.api.metadata;

import java.nio.file.Path;
import java.util.List;

/** Where a mod was loaded from: a file on disk, or a jar nested inside another mod. */
public interface ModOrigin {
	Kind getKind();

	List<Path> getPaths();

	String getParentModId();

	String getParentSubLocation();

	enum Kind {
		PATH, NESTED, UNKNOWN
	}
}
