package dev.java4u.chunky.version;

import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.Comparator;
import java.util.List;

import net.fabricmc.loader.api.SemanticVersion;
import net.fabricmc.loader.api.Version;
import net.fabricmc.loader.api.metadata.version.VersionInterval;

/** A version range. A null bound is unbounded; null intervals mean "no versions". */
public final class Interval implements VersionInterval {
	private final Version min;
	private final boolean minInclusive;
	private final Version max;
	private final boolean maxInclusive;

	public Interval(Version min, boolean minInclusive, Version max, boolean maxInclusive) {
		this.min = min;
		this.minInclusive = min != null && minInclusive;
		this.max = max;
		this.maxInclusive = max != null && maxInclusive;
	}

	@Override
	public boolean isSemantic() {
		return (min == null || min instanceof SemanticVersion) && (max == null || max instanceof SemanticVersion);
	}

	@Override
	public Version getMin() {
		return min;
	}

	@Override
	public boolean isMinInclusive() {
		return minInclusive;
	}

	@Override
	public Version getMax() {
		return max;
	}

	@Override
	public boolean isMaxInclusive() {
		return maxInclusive;
	}

	boolean contains(Version v) {
		if (min != null) {
			int c = v.compareTo(min);
			if (c < 0 || (c == 0 && !minInclusive)) return false;
		}
		if (max != null) {
			int c = v.compareTo(max);
			if (c > 0 || (c == 0 && !maxInclusive)) return false;
		}
		return true;
	}

	public static VersionInterval intersect(VersionInterval a, VersionInterval b) {
		if (a == null || b == null) return null;
		Version min;
		boolean minInc;
		if (a.getMin() == null) {
			min = b.getMin();
			minInc = b.isMinInclusive();
		} else if (b.getMin() == null) {
			min = a.getMin();
			minInc = a.isMinInclusive();
		} else {
			int c = a.getMin().compareTo(b.getMin());
			min = c >= 0 ? a.getMin() : b.getMin();
			minInc = c > 0 ? a.isMinInclusive() : c < 0 ? b.isMinInclusive() : a.isMinInclusive() && b.isMinInclusive();
		}
		Version max;
		boolean maxInc;
		if (a.getMax() == null) {
			max = b.getMax();
			maxInc = b.isMaxInclusive();
		} else if (b.getMax() == null) {
			max = a.getMax();
			maxInc = a.isMaxInclusive();
		} else {
			int c = a.getMax().compareTo(b.getMax());
			max = c <= 0 ? a.getMax() : b.getMax();
			maxInc = c < 0 ? a.isMaxInclusive() : c > 0 ? b.isMaxInclusive() : a.isMaxInclusive() && b.isMaxInclusive();
		}
		if (min != null && max != null) {
			int c = min.compareTo(max);
			if (c > 0 || (c == 0 && !(minInc && maxInc))) return null;
		}
		return new Interval(min, minInc, max, maxInc);
	}

	public static List<VersionInterval> intersectAll(Collection<VersionInterval> a, Collection<VersionInterval> b) {
		List<VersionInterval> out = new ArrayList<>();
		for (VersionInterval x : a) {
			for (VersionInterval y : b) {
				VersionInterval z = intersect(x, y);
				if (z != null) out.add(z);
			}
		}
		return merge(out);
	}

	public static List<VersionInterval> union(Collection<VersionInterval> a, VersionInterval b) {
		List<VersionInterval> all = new ArrayList<>(a);
		if (b != null) all.add(b);
		return merge(all);
	}

	/** Versions not covered by any of the intervals. */
	public static List<VersionInterval> complement(Collection<VersionInterval> intervals) {
		List<VersionInterval> sorted = merge(new ArrayList<>(intervals));
		List<VersionInterval> out = new ArrayList<>();
		if (sorted.isEmpty()) {
			out.add(INFINITE);
			return out;
		}
		Version prevMax = null;
		boolean prevMaxInc = false;
		boolean first = true;
		for (VersionInterval i : sorted) {
			if (first) {
				if (i.getMin() != null) out.add(new Interval(null, false, i.getMin(), !i.isMinInclusive()));
				first = false;
			} else {
				out.add(new Interval(prevMax, !prevMaxInc, i.getMin(), !i.isMinInclusive()));
			}
			prevMax = i.getMax();
			prevMaxInc = i.isMaxInclusive();
			if (prevMax == null) return out;
		}
		out.add(new Interval(prevMax, !prevMaxInc, null, false));
		return out;
	}

	/** Sorts intervals by lower bound and joins any that overlap. */
	private static List<VersionInterval> merge(List<VersionInterval> list) {
		list.removeIf(java.util.Objects::isNull);
		if (list.size() < 2) return list;
		list.sort(Comparator.comparing(VersionInterval::getMin, Comparator.nullsFirst(Comparator.naturalOrder())));
		List<VersionInterval> out = new ArrayList<>();
		VersionInterval cur = list.get(0);
		for (int i = 1; i < list.size(); i++) {
			VersionInterval next = list.get(i);
			boolean overlaps = cur.getMax() == null || next.getMin() == null
					|| next.getMin().compareTo(cur.getMax()) < 0
					|| (next.getMin().compareTo(cur.getMax()) == 0 && (next.isMinInclusive() || cur.isMaxInclusive()));
			if (overlaps) {
				Version max;
				boolean maxInc;
				if (cur.getMax() == null || next.getMax() == null) {
					max = null;
					maxInc = false;
				} else {
					int c = cur.getMax().compareTo(next.getMax());
					max = c >= 0 ? cur.getMax() : next.getMax();
					maxInc = c > 0 ? cur.isMaxInclusive() : c < 0 ? next.isMaxInclusive() : cur.isMaxInclusive() || next.isMaxInclusive();
				}
				cur = new Interval(cur.getMin(), cur.isMinInclusive(), max, maxInc);
			} else {
				out.add(cur);
				cur = next;
			}
		}
		out.add(cur);
		return Collections.unmodifiableList(out);
	}

	@Override
	public String toString() {
		return (min == null ? "(-inf" : (minInclusive ? "[" : "(") + min) + ","
				+ (max == null ? "inf)" : max + (maxInclusive ? "]" : ")"));
	}
}
