package dev.java4u.chunky.mod;

import java.util.ArrayList;
import java.util.Collection;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;

import net.fabricmc.api.EnvType;
import net.fabricmc.loader.api.metadata.ModDependency;
import dev.java4u.chunky.Log;

/**
 * Chooses which mods load: drops mods for the other side, keeps the newest copy when the
 * same mod appears twice, checks dependencies, and orders mods so dependencies come first.
 */
public final class Resolver {
	/** Raised when mods can't load together. The message is meant for players. */
	public static final class ResolutionException extends Exception {
		public ResolutionException(String message) {
			super(message);
		}
	}

	private Resolver() {
	}

	public static List<ChunkyMod> resolve(List<ChunkyMod> builtins, List<ChunkyMod> candidates, EnvType env)
			throws ResolutionException {
		Map<String, ChunkyMod> chosen = new LinkedHashMap<>();
		for (ChunkyMod b : builtins) chosen.put(b.id(), b);

		for (ChunkyMod m : candidates) {
			if (!m.meta.getEnvironment().matches(env)) {
				Log.info("Skipping " + m.meta + " (it's for the " + m.meta.getEnvironment().name().toLowerCase() + " only)");
				continue;
			}
			ChunkyMod existing = chosen.get(m.id());
			if (existing == null) {
				chosen.put(m.id(), m);
			} else if (existing.meta.getType().equals("builtin")) {
				Log.info("Using Chunky's built-in " + existing.id() + " instead of the copy in " + m.getOrigin());
			} else if (m.meta.getVersion().compareTo(existing.meta.getVersion()) > 0) {
				Log.info("Using " + m.meta + " instead of " + existing.meta.getVersion().getFriendlyString());
				chosen.put(m.id(), m);
			}
		}

		// Map every id a mod answers to (its own and any it "provides") to that mod.
		Map<String, ChunkyMod> providers = new LinkedHashMap<>();
		for (ChunkyMod m : chosen.values()) {
			providers.put(m.id(), m);
		}
		for (ChunkyMod m : chosen.values()) {
			for (String alias : m.meta.getProvides()) providers.putIfAbsent(alias, m);
		}

		List<String> problems = new ArrayList<>();
		for (ChunkyMod m : chosen.values()) {
			for (ModDependency d : m.meta.getDependencies()) {
				ChunkyMod other = providers.get(d.getModId());
				String who = m.meta.getName() + " (" + m.id() + ")";
				switch (d.getKind()) {
				case DEPENDS:
					if (other == null) {
						problems.add(who + " needs " + d + ", which isn't installed.");
					} else if (!d.matches(other.meta.getVersion())) {
						problems.add(who + " needs " + d + ", but " + other.meta + " is installed.");
					}
					break;
				case BREAKS:
					if (other != null && d.matches(other.meta.getVersion())) {
						problems.add(who + " doesn't work with " + other.meta + ". Remove one of them.");
					}
					break;
				case CONFLICTS:
					if (other != null && d.matches(other.meta.getVersion())) {
						Log.warn(who + " may have problems with " + other.meta);
					}
					break;
				case RECOMMENDS:
					if (other == null) Log.info(who + " recommends installing " + d);
					break;
				default:
					break;
				}
			}
		}
		if (!problems.isEmpty()) {
			throw new ResolutionException("Chunky can't start these mods together:\n - " + String.join("\n - ", problems));
		}
		return order(chosen.values(), providers);
	}

	/** Dependencies before dependents; otherwise keeps discovery order. Cycles are tolerated. */
	private static List<ChunkyMod> order(Collection<ChunkyMod> mods, Map<String, ChunkyMod> providers) {
		List<ChunkyMod> out = new ArrayList<>();
		Set<ChunkyMod> done = new HashSet<>();
		Set<ChunkyMod> visiting = new HashSet<>();
		for (ChunkyMod m : mods) visit(m, providers, done, visiting, out);
		return out;
	}

	private static void visit(ChunkyMod m, Map<String, ChunkyMod> providers, Set<ChunkyMod> done,
			Set<ChunkyMod> visiting, List<ChunkyMod> out) {
		if (done.contains(m) || !visiting.add(m)) return;
		for (ModDependency d : m.meta.getDependencies()) {
			if (d.getKind() != ModDependency.Kind.DEPENDS) continue;
			ChunkyMod dep = providers.get(d.getModId());
			if (dep != null) visit(dep, providers, done, visiting, out);
		}
		visiting.remove(m);
		done.add(m);
		out.add(m);
	}
}
