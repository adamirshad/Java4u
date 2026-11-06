package dev.java4u.chunky.version;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

import net.fabricmc.loader.api.SemanticVersion;
import net.fabricmc.loader.api.Version;
import net.fabricmc.loader.api.VersionParsingException;
import net.fabricmc.loader.api.metadata.version.VersionComparisonOperator;
import net.fabricmc.loader.api.metadata.version.VersionInterval;
import net.fabricmc.loader.api.metadata.version.VersionPredicate;

/** Parses version requirements: space-separated terms that must all match. */
public final class Predicates {
	private Predicates() {
	}

	private static final VersionComparisonOperator[] OPERATORS = {
			// Two-character operators first so ">=" isn't read as ">".
			VersionComparisonOperator.GREATER_EQUAL, VersionComparisonOperator.LESS_EQUAL,
			VersionComparisonOperator.GREATER, VersionComparisonOperator.LESS, VersionComparisonOperator.EQUAL,
			VersionComparisonOperator.SAME_TO_NEXT_MINOR, VersionComparisonOperator.SAME_TO_NEXT_MAJOR,
	};

	public static VersionPredicate parse(String text) throws VersionParsingException {
		String s = text == null ? "" : text.trim();
		if (s.isEmpty() || s.equals("*")) return new Pred(Collections.emptyList(), s.isEmpty() ? "*" : s);
		List<Term> terms = new ArrayList<>();
		for (String raw : s.split("\\s+")) {
			VersionComparisonOperator op = VersionComparisonOperator.EQUAL;
			String rest = raw;
			for (VersionComparisonOperator o : OPERATORS) {
				if (raw.startsWith(o.getSerialized())) {
					op = o;
					rest = raw.substring(o.getSerialized().length());
					break;
				}
			}
			if (rest.isEmpty()) throw new VersionParsingException("missing version after operator in '" + text + "'");
			Version ref;
			try {
				ref = Versions.parseSemantic(rest, true);
			} catch (VersionParsingException e) {
				if (op != VersionComparisonOperator.EQUAL) throw e;
				ref = Versions.parse(rest);
			}
			if (ref instanceof SemanticVersion && ((SemanticVersion) ref).hasWildcard()
					&& op != VersionComparisonOperator.EQUAL) {
				throw new VersionParsingException("wildcards only work without an operator: '" + raw + "'");
			}
			terms.add(new Term(op, ref));
		}
		return new Pred(terms, s);
	}

	static final class Term implements VersionPredicate.PredicateTerm {
		final VersionComparisonOperator op;
		final Version ref;

		Term(VersionComparisonOperator op, Version ref) {
			this.op = op;
			this.ref = ref;
		}

		@Override
		public VersionComparisonOperator getOperator() {
			return op;
		}

		@Override
		public Version getReferenceVersion() {
			return ref;
		}

		boolean test(Version v) {
			if (ref instanceof SemanticVersion && ((SemanticVersion) ref).hasWildcard()) {
				if (!(v instanceof SemanticVersion)) return false;
				SemanticVersion sv = (SemanticVersion) v;
				List<Integer> fixed = Versions.fixedComponents((SemanticVersion) ref);
				for (int i = 0; i < fixed.size(); i++) {
					if (sv.getVersionComponent(i) != fixed.get(i)) return false;
				}
				return true;
			}
			return op.test(v, ref);
		}

		VersionInterval interval() {
			if (!(ref instanceof SemanticVersion)) return new Interval(ref, true, ref, true);
			SemanticVersion r = (SemanticVersion) ref;
			if (r.hasWildcard()) return new Interval(Versions.wildcardMin(r), true, Versions.wildcardMax(r), false);
			return new Interval(op.minVersion(r), op.isMinInclusive(), op.maxVersion(r), op.isMaxInclusive());
		}

		@Override
		public String toString() {
			return (op == VersionComparisonOperator.EQUAL ? "" : op.getSerialized()) + ref.getFriendlyString();
		}
	}

	static final class Pred implements VersionPredicate {
		private final List<Term> terms;
		private final String text;

		Pred(List<Term> terms, String text) {
			this.terms = terms;
			this.text = text;
		}

		@Override
		public boolean test(Version v) {
			for (Term t : terms) {
				if (!t.test(v)) return false;
			}
			return true;
		}

		@Override
		public List<Term> getTerms() {
			return terms;
		}

		@Override
		public VersionInterval getInterval() {
			VersionInterval out = VersionInterval.INFINITE;
			for (Term t : terms) {
				out = Interval.intersect(out, t.interval());
				if (out == null) return null;
			}
			return out;
		}

		@Override
		public String toString() {
			return text;
		}
	}
}
