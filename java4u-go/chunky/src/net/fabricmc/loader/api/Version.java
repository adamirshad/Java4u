package net.fabricmc.loader.api;

/** A mod or game version. Semantic versions implement {@link SemanticVersion}. */
public interface Version extends Comparable<Version> {
	String getFriendlyString();

	static Version parse(String text) throws VersionParsingException {
		return dev.java4u.chunky.version.Versions.parse(text);
	}
}
