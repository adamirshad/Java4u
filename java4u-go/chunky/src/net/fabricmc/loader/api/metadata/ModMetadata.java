package net.fabricmc.loader.api.metadata;

import java.util.ArrayList;
import java.util.Collection;
import java.util.List;
import java.util.Map;
import java.util.Optional;

import net.fabricmc.loader.api.Version;

/** Everything a mod declares about itself in its metadata file. */
public interface ModMetadata {
	String getType();

	String getId();

	Collection<String> getProvides();

	Version getVersion();

	ModEnvironment getEnvironment();

	Collection<ModDependency> getDependencies();

	@Deprecated
	default Collection<ModDependency> getDepends() {
		return ofKind(getDependencies(), ModDependency.Kind.DEPENDS);
	}

	@Deprecated
	default Collection<ModDependency> getRecommends() {
		return ofKind(getDependencies(), ModDependency.Kind.RECOMMENDS);
	}

	@Deprecated
	default Collection<ModDependency> getSuggests() {
		return ofKind(getDependencies(), ModDependency.Kind.SUGGESTS);
	}

	@Deprecated
	default Collection<ModDependency> getConflicts() {
		return ofKind(getDependencies(), ModDependency.Kind.CONFLICTS);
	}

	@Deprecated
	default Collection<ModDependency> getBreaks() {
		return ofKind(getDependencies(), ModDependency.Kind.BREAKS);
	}

	String getName();

	String getDescription();

	Collection<Person> getAuthors();

	Collection<Person> getContributors();

	ContactInformation getContact();

	Collection<String> getLicense();

	Optional<String> getIconPath(int size);

	@Deprecated
	boolean containsCustomElement(String key);

	boolean containsCustomValue(String key);

	CustomValue getCustomValue(String key);

	Map<String, CustomValue> getCustomValues();

	private static Collection<ModDependency> ofKind(Collection<ModDependency> all, ModDependency.Kind kind) {
		List<ModDependency> out = new ArrayList<>();
		for (ModDependency d : all) {
			if (d.getKind() == kind) out.add(d);
		}
		return out;
	}
}
