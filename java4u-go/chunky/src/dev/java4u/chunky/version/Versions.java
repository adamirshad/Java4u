package dev.java4u.chunky.version;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.Optional;

import net.fabricmc.loader.api.SemanticVersion;
import net.fabricmc.loader.api.Version;
import net.fabricmc.loader.api.VersionParsingException;

/** Chunky's version parsing and comparison. */
public final class Versions {
	private Versions() {
	}

	/** Parses a semantic version if possible, otherwise keeps the text as an opaque version. */
	public static Version parse(String text) throws VersionParsingException {
		if (text == null || text.isEmpty()) throw new VersionParsingException("empty version");
		try {
			return parseSemantic(text, false);
		} catch (VersionParsingException e) {
			return new StringVersion(text);
		}
	}

	/** Like {@link #parse} but never fails; used for metadata where a bad version must not stop loading. */
	public static Version parseLenient(String text) {
		try {
			return parse(text);
		} catch (VersionParsingException e) {
			return new StringVersion(text == null ? "" : text);
		}
	}

	public static SemanticVersion parseSemantic(String text, boolean allowWildcard) throws VersionParsingException {
		if (text == null || text.isEmpty()) throw new VersionParsingException("empty version");
		String rest = text;
		String build = null;
		int plus = rest.indexOf('+');
		if (plus >= 0) {
			build = rest.substring(plus + 1);
			rest = rest.substring(0, plus);
			if (build.isEmpty() || !validIdentifiers(build)) throw new VersionParsingException("bad build metadata in " + text);
		}
		String pre = null;
		int dash = rest.indexOf('-');
		if (dash >= 0) {
			pre = rest.substring(dash + 1);
			rest = rest.substring(0, dash);
			if (!pre.isEmpty() && !validIdentifiers(pre)) throw new VersionParsingException("bad pre-release in " + text);
		}
		if (rest.isEmpty() || rest.startsWith(".") || rest.endsWith(".") || rest.contains("..")) {
			throw new VersionParsingException("bad version core in " + text);
		}
		String[] parts = rest.split("\\.");
		int[] comps = new int[parts.length];
		boolean wildcard = false;
		for (int i = 0; i < parts.length; i++) {
			String p = parts[i];
			if (allowWildcard && (p.equals("x") || p.equals("X") || p.equals("*"))) {
				comps[i] = SemanticVersion.COMPONENT_WILDCARD;
				wildcard = true;
				continue;
			}
			if (wildcard) throw new VersionParsingException("numbers can't follow a wildcard in " + text);
			if (p.isEmpty() || !p.chars().allMatch(Character::isDigit)) {
				throw new VersionParsingException("not a number: '" + p + "' in " + text);
			}
			try {
				comps[i] = Integer.parseInt(p);
			} catch (NumberFormatException e) {
				throw new VersionParsingException("number too large in " + text);
			}
		}
		if (wildcard && (pre != null || build != null)) {
			throw new VersionParsingException("wildcards can't have pre-release or build parts: " + text);
		}
		return new SemVer(comps, pre, build, text);
	}

	private static boolean validIdentifiers(String s) {
		for (int i = 0; i < s.length(); i++) {
			char c = s.charAt(i);
			if (!(Character.isLetterOrDigit(c) && c < 128) && c != '.' && c != '-') return false;
		}
		return true;
	}

	public static int compareSemantic(SemanticVersion a, SemanticVersion b) {
		int n = Math.max(a.getVersionComponentCount(), b.getVersionComponentCount());
		for (int i = 0; i < n; i++) {
			int x = a.getVersionComponent(i), y = b.getVersionComponent(i);
			if (x == SemanticVersion.COMPONENT_WILDCARD || y == SemanticVersion.COMPONENT_WILDCARD) continue;
			if (x != y) return Integer.compare(x, y);
		}
		Optional<String> pa = a.getPrereleaseKey(), pb = b.getPrereleaseKey();
		if (pa.isEmpty() && pb.isEmpty()) return 0;
		if (pa.isEmpty()) return 1; // a release is newer than any of its pre-releases
		if (pb.isEmpty()) return -1;
		return comparePrerelease(pa.get(), pb.get());
	}

	private static int comparePrerelease(String a, String b) {
		if (a.equals(b)) return 0;
		if (a.isEmpty()) return -1; // "1.0-" sorts before every other pre-release
		if (b.isEmpty()) return 1;
		String[] x = a.split("\\."), y = b.split("\\.");
		for (int i = 0; i < Math.min(x.length, y.length); i++) {
			boolean nx = isNumber(x[i]), ny = isNumber(y[i]);
			int c;
			if (nx && ny) c = Long.compare(Long.parseLong(x[i]), Long.parseLong(y[i]));
			else if (nx) c = -1;
			else if (ny) c = 1;
			else c = x[i].compareTo(y[i]);
			if (c != 0) return c;
		}
		return Integer.compare(x.length, y.length);
	}

	private static boolean isNumber(String s) {
		return !s.isEmpty() && s.length() < 18 && s.chars().allMatch(Character::isDigit);
	}

	/** The smallest version above every version whose first (index+1) components match v. */
	public static SemanticVersion bump(SemanticVersion v, int index) {
		int[] comps = new int[index + 1];
		for (int i = 0; i < index; i++) comps[i] = v.getVersionComponent(i);
		comps[index] = v.getVersionComponent(index) + 1;
		return new SemVer(comps, "", null, null);
	}

	/** The lowest version matching a wildcard such as 1.20.x (that is, 1.20.0-). */
	static SemanticVersion wildcardMin(SemanticVersion v) {
		List<Integer> fixed = fixedComponents(v);
		int[] comps = new int[Math.max(fixed.size(), 1)];
		for (int i = 0; i < fixed.size(); i++) comps[i] = fixed.get(i);
		return new SemVer(comps, "", null, null);
	}

	/** The first version past a wildcard such as 1.20.x (that is, 1.21.0-), or null for "x". */
	static SemanticVersion wildcardMax(SemanticVersion v) {
		List<Integer> fixed = fixedComponents(v);
		if (fixed.isEmpty()) return null;
		int[] comps = new int[fixed.size()];
		for (int i = 0; i < fixed.size(); i++) comps[i] = fixed.get(i);
		comps[comps.length - 1]++;
		return new SemVer(comps, "", null, null);
	}

	static List<Integer> fixedComponents(SemanticVersion v) {
		List<Integer> out = new ArrayList<>();
		for (int i = 0; i < v.getVersionComponentCount(); i++) {
			int c = v.getVersionComponent(i);
			if (c == SemanticVersion.COMPONENT_WILDCARD) break;
			out.add(c);
		}
		return out;
	}

	/** A semantic version. */
	static final class SemVer implements SemanticVersion {
		private final int[] comps;
		private final String pre;
		private final String build;
		private final String text;

		SemVer(int[] comps, String pre, String build, String text) {
			this.comps = comps;
			this.pre = pre;
			this.build = build;
			this.text = text;
		}

		@Override
		public int getVersionComponentCount() {
			return comps.length;
		}

		@Override
		public int getVersionComponent(int i) {
			if (i < comps.length) return comps[i];
			// Past the end: a trailing wildcard keeps matching, otherwise missing parts are zero.
			return comps.length > 0 && comps[comps.length - 1] == COMPONENT_WILDCARD ? COMPONENT_WILDCARD : 0;
		}

		@Override
		public Optional<String> getPrereleaseKey() {
			return Optional.ofNullable(pre);
		}

		@Override
		public Optional<String> getBuildKey() {
			return Optional.ofNullable(build);
		}

		@Override
		public boolean hasWildcard() {
			for (int c : comps) if (c == COMPONENT_WILDCARD) return true;
			return false;
		}

		@Override
		public String getFriendlyString() {
			if (text != null) return text;
			StringBuilder sb = new StringBuilder();
			for (int i = 0; i < comps.length; i++) {
				if (i > 0) sb.append('.');
				sb.append(comps[i] == COMPONENT_WILDCARD ? "x" : Integer.toString(comps[i]));
			}
			if (pre != null) sb.append('-').append(pre);
			if (build != null) sb.append('+').append(build);
			return sb.toString();
		}

		@Override
		public int compareTo(Version o) {
			if (o instanceof SemanticVersion) return compareSemantic(this, (SemanticVersion) o);
			return getFriendlyString().compareTo(o.getFriendlyString());
		}

		@Override
		public boolean equals(Object o) {
			if (!(o instanceof SemVer)) return false;
			SemVer v = (SemVer) o;
			return compareSemantic(this, v) == 0 && java.util.Objects.equals(build, v.build);
		}

		@Override
		public int hashCode() {
			int n = comps.length;
			while (n > 1 && comps[n - 1] == 0) n--; // 1.0 and 1.0.0 are equal
			return Arrays.hashCode(Arrays.copyOf(comps, n)) * 31 + java.util.Objects.hashCode(pre);
		}

		@Override
		public String toString() {
			return getFriendlyString();
		}
	}

	/** A version that isn't semantic; it only supports equality and plain text ordering. */
	static final class StringVersion implements Version {
		private final String text;

		StringVersion(String text) {
			this.text = text;
		}

		@Override
		public String getFriendlyString() {
			return text;
		}

		@Override
		public int compareTo(Version o) {
			return text.compareTo(o.getFriendlyString());
		}

		@Override
		public boolean equals(Object o) {
			return o instanceof StringVersion && ((StringVersion) o).text.equals(text);
		}

		@Override
		public int hashCode() {
			return text.hashCode();
		}

		@Override
		public String toString() {
			return text;
		}
	}
}
