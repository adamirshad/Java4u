package net.fabricmc.loader.api;

import java.util.Optional;

/** A version in "major.minor.patch-prerelease+build" form. */
public interface SemanticVersion extends Version {
	/** Value of a component written as "x" or "*" in a version range. */
	int COMPONENT_WILDCARD = Integer.MIN_VALUE;

	int getVersionComponentCount();

	int getVersionComponent(int index);

	Optional<String> getPrereleaseKey();

	Optional<String> getBuildKey();

	boolean hasWildcard();

	@Deprecated
	default int compareTo(SemanticVersion other) {
		return dev.java4u.chunky.version.Versions.compareSemantic(this, other);
	}

	static SemanticVersion parse(String text) throws VersionParsingException {
		return dev.java4u.chunky.version.Versions.parseSemantic(text, false);
	}
}
