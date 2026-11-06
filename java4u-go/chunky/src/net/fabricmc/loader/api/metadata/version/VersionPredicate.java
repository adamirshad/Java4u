package net.fabricmc.loader.api.metadata.version;

import java.util.ArrayList;
import java.util.Collection;
import java.util.List;
import java.util.function.Predicate;

import net.fabricmc.loader.api.Version;
import net.fabricmc.loader.api.VersionParsingException;
import dev.java4u.chunky.version.Predicates;

/** A parsed version requirement such as ">=1.2 <2" or "1.20.x". */
public interface VersionPredicate extends Predicate<Version> {
	Collection<? extends PredicateTerm> getTerms();

	VersionInterval getInterval();

	interface PredicateTerm {
		VersionComparisonOperator getOperator();

		Version getReferenceVersion();
	}

	static VersionPredicate parse(String text) throws VersionParsingException {
		return Predicates.parse(text);
	}

	static Collection<VersionPredicate> parse(Collection<String> texts) throws VersionParsingException {
		List<VersionPredicate> out = new ArrayList<>(texts.size());
		for (String t : texts) out.add(parse(t));
		return out;
	}
}
