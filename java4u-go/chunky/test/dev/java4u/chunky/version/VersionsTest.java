package dev.java4u.chunky.version;

import static dev.java4u.chunky.test.Check.*;

import net.fabricmc.loader.api.SemanticVersion;
import net.fabricmc.loader.api.Version;
import net.fabricmc.loader.api.VersionParsingException;
import net.fabricmc.loader.api.metadata.version.VersionInterval;
import net.fabricmc.loader.api.metadata.version.VersionPredicate;

public final class VersionsTest {
	private VersionsTest() {
	}

	private static Version v(String s) {
		return Versions.parseLenient(s);
	}

	private static boolean matches(String range, String version) throws VersionParsingException {
		return Predicates.parse(range).test(v(version));
	}

	public static void testParsesSemanticVersions() throws Exception {
		SemanticVersion s = Versions.parseSemantic("1.21.4-beta.2+build.7", false);
		eq(3, s.getVersionComponentCount(), "components");
		eq(21, s.getVersionComponent(1), "minor");
		eq("beta.2", s.getPrereleaseKey().orElse(null), "pre-release");
		eq("build.7", s.getBuildKey().orElse(null), "build");
		isTrue(v("26.3") instanceof SemanticVersion, "26.3 is semantic");
		isFalse(v("1.0_beta") instanceof SemanticVersion, "1.0_beta is kept as text");
		eq("1.0_beta", v("1.0_beta").getFriendlyString(), "text version");
	}

	public static void testRejectsBadVersions() {
		for (String bad : new String[] { "", "1..2", ".1", "1.", "1.a", "1.0-be ta", "1.0+" }) {
			fails(VersionParsingException.class, () -> Versions.parseSemantic(bad, false), "'" + bad + "'");
		}
		fails(VersionParsingException.class, () -> Versions.parseSemantic("1.x", false), "wildcard not allowed");
		fails(VersionParsingException.class, () -> Versions.parseSemantic("1.x.2", true), "number after wildcard");
		fails(VersionParsingException.class, () -> Versions.parseSemantic("1.x-beta", true), "wildcard with pre-release");
	}

	public static void testOrdersVersions() {
		String[] ascending = { "1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-beta.2", "1.0.0-beta.10", "1.0.0", "1.2", "1.10.0", "2" };
		for (int i = 0; i + 1 < ascending.length; i++) {
			isTrue(v(ascending[i]).compareTo(v(ascending[i + 1])) < 0, ascending[i] + " < " + ascending[i + 1]);
			isTrue(v(ascending[i + 1]).compareTo(v(ascending[i])) > 0, ascending[i + 1] + " > " + ascending[i]);
		}
		eq(0, v("1.0.0+a").compareTo(v("1.0.0+b")), "build metadata is ignored");
		eq(0, v("1.2").compareTo(v("1.2.0")), "missing components count as 0");
	}

	public static void testRanges() throws Exception {
		isTrue(matches(">=1.2 <2", "1.5.3"), ">=1.2 <2 has 1.5.3");
		isFalse(matches(">=1.2 <2", "2.0.0"), ">=1.2 <2 excludes 2.0.0");
		isFalse(matches(">=1.2 <2", "1.1"), ">=1.2 <2 excludes 1.1");
		isTrue(matches("<=1.2", "1.2"), "<= includes the bound");
		isFalse(matches(">1.2", "1.2"), "> excludes the bound");
		isTrue(matches("1.2.3", "1.2.3"), "bare version means equal");
		isFalse(matches("=1.2.3", "1.2.4"), "= is exact");
	}

	public static void testTildeAndCaret() throws Exception {
		isTrue(matches("~1.2", "1.2.9"), "~1.2 has 1.2.9");
		isFalse(matches("~1.2", "1.3.0"), "~1.2 stops before 1.3");
		isFalse(matches("~1.2.3", "1.2.2"), "~1.2.3 starts at 1.2.3");
		isTrue(matches("^1.2", "1.9.0"), "^1.2 has 1.9");
		isFalse(matches("^1.2", "2.0.0"), "^1.2 stops before 2");
		isFalse(matches("^1.2", "1.1.0"), "^1.2 starts at 1.2");
	}

	public static void testWildcards() throws Exception {
		isTrue(matches("1.21.x", "1.21.4"), "1.21.x has 1.21.4");
		isTrue(matches("1.21.x", "1.21"), "1.21.x has 1.21");
		isFalse(matches("1.21.x", "1.22.0"), "1.21.x excludes 1.22");
		isTrue(matches("*", "999.0"), "* matches anything");
		isTrue(matches("", "1.0"), "an empty range matches anything");
		fails(VersionParsingException.class, () -> Predicates.parse(">=1.x"), "wildcard with an operator");
		fails(VersionParsingException.class, () -> Predicates.parse(">="), "operator without a version");
	}

	public static void testTextVersionsOnlyMatchExactly() throws Exception {
		isTrue(matches("1.0_beta", "1.0_beta"), "same text");
		isFalse(matches("1.0_beta", "1.0_gamma"), "other text");
		fails(VersionParsingException.class, () -> Predicates.parse(">=1.0_beta"), "operator on a text version");
	}

	public static void testIntervals() throws Exception {
		VersionInterval i = Predicates.parse(">=1.2 <2").getInterval();
		eq("1.2", i.getMin().getFriendlyString(), "min");
		isTrue(i.isMinInclusive(), "min inclusive");
		eq("2", i.getMax().getFriendlyString(), "max");
		isFalse(i.isMaxInclusive(), "max exclusive");
		eq(null, Predicates.parse(">=2 <1").getInterval(), "impossible range has no interval");

		// ^1.2 ends just below 2's first pre-release, so 2.0.0-alpha is outside it too.
		VersionInterval caret = Predicates.parse("^1.2").getInterval();
		isFalse(caret.isMaxInclusive(), "^1.2 max is exclusive");
		isTrue(caret.getMax().compareTo(v("1.99.99")) > 0, "^1.2 max is above 1.99.99");
		isTrue(caret.getMax().compareTo(v("2.0.0-alpha")) < 0, "^1.2 max is below 2.0.0-alpha");
		isTrue(VersionPredicate.parse("*").getInterval() != null, "* has an interval");
	}
}
