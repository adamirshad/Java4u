package net.fabricmc.loader.api.metadata.version;

import java.util.Collection;
import java.util.Collections;
import java.util.List;

import net.fabricmc.loader.api.Version;
import dev.java4u.chunky.version.Interval;

/** A range of versions with optional lower and upper bounds. Null bounds mean unbounded. */
public interface VersionInterval {
	VersionInterval INFINITE = new Interval(null, false, null, false);

	boolean isSemantic();

	Version getMin();

	boolean isMinInclusive();

	Version getMax();

	boolean isMaxInclusive();

	default VersionInterval and(VersionInterval other) {
		return and(this, other);
	}

	default List<VersionInterval> or(Collection<VersionInterval> others) {
		return or(others, this);
	}

	default List<VersionInterval> not() {
		return not(this);
	}

	static VersionInterval and(VersionInterval a, VersionInterval b) {
		return Interval.intersect(a, b);
	}

	static List<VersionInterval> and(Collection<VersionInterval> a, Collection<VersionInterval> b) {
		return Interval.intersectAll(a, b);
	}

	static List<VersionInterval> or(Collection<VersionInterval> a, VersionInterval b) {
		return Interval.union(a, b);
	}

	static List<VersionInterval> not(VersionInterval interval) {
		return Interval.complement(interval == null ? Collections.emptyList() : Collections.singletonList(interval));
	}

	static List<VersionInterval> not(Collection<VersionInterval> intervals) {
		return Interval.complement(intervals);
	}
}
