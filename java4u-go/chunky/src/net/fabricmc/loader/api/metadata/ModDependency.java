package net.fabricmc.loader.api.metadata;

import java.util.Collection;
import java.util.List;

import net.fabricmc.loader.api.Version;
import net.fabricmc.loader.api.metadata.version.VersionInterval;
import net.fabricmc.loader.api.metadata.version.VersionPredicate;

/** One entry from a mod's depends/recommends/suggests/conflicts/breaks lists. */
public interface ModDependency {
	Kind getKind();

	String getModId();

	boolean matches(Version version);

	Collection<VersionPredicate> getVersionRequirements();

	List<VersionInterval> getVersionIntervals();

	enum Kind {
		DEPENDS("depends", true, false),
		RECOMMENDS("recommends", true, true),
		SUGGESTS("suggests", true, true),
		CONFLICTS("conflicts", false, true),
		BREAKS("breaks", false, false);

		private final String key;
		private final boolean positive;
		private final boolean soft;

		Kind(String key, boolean positive, boolean soft) {
			this.key = key;
			this.positive = positive;
			this.soft = soft;
		}

		public String getKey() {
			return key;
		}

		/** True for kinds that want the other mod present. */
		public boolean isPositive() {
			return positive;
		}

		/** True for kinds that only warn instead of stopping the launch. */
		public boolean isSoft() {
			return soft;
		}

		public static Kind parse(String key) {
			for (Kind k : values()) {
				if (k.key.equals(key)) return k;
			}
			return null;
		}
	}
}
