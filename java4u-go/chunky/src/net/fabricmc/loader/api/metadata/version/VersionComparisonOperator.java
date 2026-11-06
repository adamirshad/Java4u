package net.fabricmc.loader.api.metadata.version;

import net.fabricmc.loader.api.SemanticVersion;
import net.fabricmc.loader.api.Version;
import dev.java4u.chunky.version.Versions;

/** The operators allowed in version ranges such as ">=1.2", "~1.2" or "^1". */
public enum VersionComparisonOperator {
	GREATER_EQUAL(">=", true, false) {
		@Override
		public boolean test(SemanticVersion a, SemanticVersion b) {
			return Versions.compareSemantic(a, b) >= 0;
		}

		@Override
		public SemanticVersion minVersion(SemanticVersion v) {
			return v;
		}
	},
	LESS_EQUAL("<=", false, true) {
		@Override
		public boolean test(SemanticVersion a, SemanticVersion b) {
			return Versions.compareSemantic(a, b) <= 0;
		}

		@Override
		public SemanticVersion maxVersion(SemanticVersion v) {
			return v;
		}
	},
	GREATER(">", false, false) {
		@Override
		public boolean test(SemanticVersion a, SemanticVersion b) {
			return Versions.compareSemantic(a, b) > 0;
		}

		@Override
		public SemanticVersion minVersion(SemanticVersion v) {
			return v;
		}
	},
	LESS("<", false, false) {
		@Override
		public boolean test(SemanticVersion a, SemanticVersion b) {
			return Versions.compareSemantic(a, b) < 0;
		}

		@Override
		public SemanticVersion maxVersion(SemanticVersion v) {
			return v;
		}
	},
	EQUAL("=", true, true) {
		@Override
		public boolean test(SemanticVersion a, SemanticVersion b) {
			return Versions.compareSemantic(a, b) == 0;
		}

		@Override
		public SemanticVersion minVersion(SemanticVersion v) {
			return v;
		}

		@Override
		public SemanticVersion maxVersion(SemanticVersion v) {
			return v;
		}
	},
	SAME_TO_NEXT_MINOR("~", true, false) {
		@Override
		public boolean test(SemanticVersion a, SemanticVersion b) {
			return Versions.compareSemantic(a, b) >= 0
					&& a.getVersionComponent(0) == b.getVersionComponent(0)
					&& (b.getVersionComponentCount() < 2 || a.getVersionComponent(1) == b.getVersionComponent(1));
		}

		@Override
		public SemanticVersion minVersion(SemanticVersion v) {
			return v;
		}

		@Override
		public SemanticVersion maxVersion(SemanticVersion v) {
			return v.getVersionComponentCount() < 2
					? Versions.bump(v, 0)
					: Versions.bump(v, 1);
		}
	},
	SAME_TO_NEXT_MAJOR("^", true, false) {
		@Override
		public boolean test(SemanticVersion a, SemanticVersion b) {
			return Versions.compareSemantic(a, b) >= 0 && a.getVersionComponent(0) == b.getVersionComponent(0);
		}

		@Override
		public SemanticVersion minVersion(SemanticVersion v) {
			return v;
		}

		@Override
		public SemanticVersion maxVersion(SemanticVersion v) {
			return Versions.bump(v, 0);
		}
	};

	private final String serialized;
	private final boolean minInclusive;
	private final boolean maxInclusive;

	VersionComparisonOperator(String serialized, boolean minInclusive, boolean maxInclusive) {
		this.serialized = serialized;
		this.minInclusive = minInclusive;
		this.maxInclusive = maxInclusive;
	}

	public final String getSerialized() {
		return serialized;
	}

	public final boolean isMinInclusive() {
		return minInclusive;
	}

	public final boolean isMaxInclusive() {
		return maxInclusive;
	}

	public final boolean test(Version a, Version b) {
		if (a instanceof SemanticVersion && b instanceof SemanticVersion) {
			return test((SemanticVersion) a, (SemanticVersion) b);
		}
		// Non-semantic versions can only be compared for equality.
		return this == EQUAL && a.getFriendlyString().equals(b.getFriendlyString());
	}

	public abstract boolean test(SemanticVersion a, SemanticVersion b);

	public SemanticVersion minVersion(SemanticVersion v) {
		return null;
	}

	public SemanticVersion maxVersion(SemanticVersion v) {
		return null;
	}
}
