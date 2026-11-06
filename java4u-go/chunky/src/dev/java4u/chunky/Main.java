package dev.java4u.chunky;

import java.io.IOException;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;

import org.spongepowered.asm.launch.MixinBootstrap;
import org.spongepowered.asm.mixin.FabricUtil;
import org.spongepowered.asm.mixin.MixinEnvironment;
import org.spongepowered.asm.mixin.Mixins;
import org.spongepowered.asm.mixin.transformer.Config;
import org.spongepowered.asm.mixin.transformer.IMixinTransformer;

import net.fabricmc.api.EnvType;
import net.fabricmc.loader.api.SemanticVersion;
import net.fabricmc.loader.api.Version;
import net.fabricmc.loader.api.entrypoint.PreLaunchEntrypoint;
import net.fabricmc.loader.api.metadata.ModDependency;
import net.fabricmc.loader.api.metadata.version.VersionInterval;
import dev.java4u.chunky.loader.AccessWidener;
import dev.java4u.chunky.loader.ChunkyClassLoader;
import dev.java4u.chunky.loader.Transformer;
import dev.java4u.chunky.mixin.ChunkyMixinService;
import dev.java4u.chunky.mod.ChunkyMod;
import dev.java4u.chunky.mod.Discovery;
import dev.java4u.chunky.mod.ModMeta;
import dev.java4u.chunky.mod.Resolver;
import dev.java4u.chunky.version.Versions;

/**
 * Chunky's entry point. Java4U starts the JVM here instead of at Minecraft's main class.
 *
 * Settings come from system properties set by Java4U:
 * chunky.mainClass, chunky.gameDir, chunky.gameVersion, chunky.gameJar and
 * chunky.classpathFile (the game jar and its libraries, one path per line).
 */
public final class Main {
	public static final String VERSION = "0.2.0";
	/** The Fabric Loader API level Chunky answers to when mods depend on "fabricloader". */
	public static final String FABRIC_API_LEVEL = "0.20.1";

	private Main() {
	}

	public static void main(String[] args) throws Throwable {
		Path gameDir = Path.of(required("chunky.gameDir")).toAbsolutePath();
		Log.init(gameDir);
		try {
			run(args, gameDir);
		} catch (Resolver.ResolutionException e) {
			Log.error(e.getMessage(), null);
			System.exit(1);
		} catch (InvocationTargetException e) {
			throw e.getCause();
		} catch (Throwable t) {
			Log.error("Chunky couldn't start the game", t);
			throw t;
		}
	}

	private static void run(String[] args, Path gameDir) throws Throwable {
		String mainClass = required("chunky.mainClass");
		String gameVersion = required("chunky.gameVersion");
		Path gameJar = Path.of(required("chunky.gameJar"));
		List<Path> gameClasspath = readClasspath(Path.of(required("chunky.classpathFile")));
		long t0 = System.nanoTime();
		Log.info("Chunky " + VERSION + " starting Minecraft " + gameVersion + " on Java " + Runtime.version());

		// 1. Find and choose mods.
		List<ChunkyMod> builtins = builtins(gameJar, gameVersion);
		List<ChunkyMod> found = Discovery.scan(gameDir.resolve("mods"), gameDir.resolve(".chunky").resolve("nested"));
		List<ChunkyMod> mods = Resolver.resolve(builtins, found, EnvType.CLIENT);
		List<String> names = new ArrayList<>();
		for (ChunkyMod m : mods) if (!m.meta.getType().equals("builtin") && !m.meta.getType().equals("library")) names.add(m.meta.toString());
		Log.info("Loading " + names.size() + " mod" + (names.size() == 1 ? "" : "s") + (names.isEmpty() ? "" : ": " + String.join(", ", names)));

		// 2. One class loader for the game, its libraries and every mod.
		List<Path> classpath = new ArrayList<>(gameClasspath);
		for (ChunkyMod m : mods) {
			if (m.jar != null && !m.meta.getType().equals("builtin")) classpath.add(m.jar);
		}
		ChunkyClassLoader cl = new ChunkyClassLoader(classpath, Main.class.getClassLoader());
		Thread.currentThread().setContextClassLoader(cl);
		ChunkyLoader.init(mods, gameDir, gameVersion, args, cl);

		// 3. Access wideners and Mixin.
		AccessWidener widener = readWideners(mods);
		IMixinTransformer mixin = startMixin(cl, mods);
		cl.setTransformer(new Transformer(widener.isEmpty() ? null : widener, mixin));
		Log.info("Ready in " + (System.nanoTime() - t0) / 1_000_000 + " ms");

		// 4. Let mods run code before the game loads, then start the game.
		ChunkyLoader.get().invokeEntrypoints("preLaunch", PreLaunchEntrypoint.class, PreLaunchEntrypoint::onPreLaunch);
		Class<?> game = Class.forName(mainClass, false, cl);
		Method main = game.getMethod("main", String[].class);
		main.invoke(null, (Object) args);
	}

	private static List<ChunkyMod> builtins(Path gameJar, String gameVersion) throws Exception {
		List<ChunkyMod> out = new ArrayList<>();
		out.add(new ChunkyMod(ModMeta.builtin("minecraft", "Minecraft", gameVersion, "The base game"), gameJar, null, null));
		Runtime.Version jv = Runtime.version();
		out.add(new ChunkyMod(ModMeta.builtin("java", "Java", jv.feature() + "." + jv.interim() + "." + jv.update(),
				"The Java runtime"), null, null, null));
		Path self = Path.of(Main.class.getProtectionDomain().getCodeSource().getLocation().toURI());
		out.add(new ChunkyMod(ModMeta.builtin("chunky", "Chunky", VERSION, "Java4U's mod loader"), self, null, null));
		out.add(new ChunkyMod(ModMeta.builtin("fabricloader", "Fabric Loader API (Chunky)", FABRIC_API_LEVEL,
				"Chunky runs Fabric mods by providing the Fabric Loader API"), null, null, null));
		String mixinExtras = "0.0.0";
		try {
			mixinExtras = com.llamalad7.mixinextras.MixinExtrasBootstrap.getVersion();
		} catch (Throwable ignored) {
			// MixinExtras missing; mods that need it will report the missing dependency.
		}
		out.add(new ChunkyMod(ModMeta.builtin("mixinextras", "MixinExtras", mixinExtras, "Extra Mixin features"), null, null, null));
		return out;
	}

	private static AccessWidener readWideners(List<ChunkyMod> mods) {
		AccessWidener w = new AccessWidener();
		for (ChunkyMod m : mods) {
			String file = m.meta.accessWidener;
			if (file == null) continue;
			Optional<Path> p = m.findPath(file);
			if (p.isEmpty()) throw new IllegalStateException(m.id() + " lists access widener " + file + " but it isn't in the jar");
			try {
				w.read(Files.readString(p.get(), StandardCharsets.UTF_8), m.id() + "/" + file);
			} catch (IOException e) {
				throw new IllegalStateException("can't read " + file + " from " + m.id(), e);
			}
		}
		return w;
	}

	private static IMixinTransformer startMixin(ChunkyClassLoader cl, List<ChunkyMod> mods) throws Exception {
		ChunkyMixinService.setLoader(cl);
		MixinBootstrap.init();
		try {
			com.llamalad7.mixinextras.MixinExtrasBootstrap.init();
		} catch (Throwable t) {
			Log.warn("MixinExtras couldn't start; mods that use it may fail: " + t);
		}
		MixinEnvironment.getDefaultEnvironment().setSide(MixinEnvironment.Side.CLIENT);

		Map<String, ChunkyMod> owners = new HashMap<>();
		int count = 0;
		for (ChunkyMod m : mods) {
			for (ModMeta.MixinConfig c : m.meta.mixins) {
				if (!c.environment.matches(EnvType.CLIENT)) continue;
				owners.put(c.config, m);
				Mixins.addConfiguration(c.config);
				count++;
			}
		}
		// Tag each config with its mod, and with the Mixin behavior that mod was built for.
		for (Config config : Mixins.getConfigs()) {
			ChunkyMod owner = owners.get(config.getName());
			if (owner == null) continue;
			config.getConfig().decorate(FabricUtil.KEY_MOD_ID, owner.id());
			config.getConfig().decorate(FabricUtil.KEY_COMPATIBILITY, compatibilityFor(owner));
		}

		Method gotoPhase = MixinEnvironment.class.getDeclaredMethod("gotoPhase", MixinEnvironment.Phase.class);
		gotoPhase.setAccessible(true);
		gotoPhase.invoke(null, MixinEnvironment.Phase.INIT);
		gotoPhase.invoke(null, MixinEnvironment.Phase.DEFAULT);

		IMixinTransformer t = ChunkyMixinService.transformer();
		if (t == null) {
			Object active = MixinEnvironment.getCurrentEnvironment().getActiveTransformer();
			if (active instanceof IMixinTransformer) t = (IMixinTransformer) active;
		}
		if (t == null) throw new IllegalStateException("Mixin didn't provide a transformer");
		Log.info("Mixin " + MixinBootstrap.VERSION + " ready with " + count + " config" + (count == 1 ? "" : "s"));
		return t;
	}

	/** Mods built against older loaders get the Mixin behavior of that time. */
	private static int compatibilityFor(ChunkyMod mod) {
		for (ModDependency d : mod.meta.getDependencies()) {
			if (d.getKind() != ModDependency.Kind.DEPENDS || !d.getModId().equals("fabricloader")) continue;
			Version min = null;
			for (VersionInterval i : d.getVersionIntervals()) {
				if (i.getMin() == null) return FabricUtil.COMPATIBILITY_LATEST;
				if (min == null || i.getMin().compareTo(min) < 0) min = i.getMin();
			}
			if (!(min instanceof SemanticVersion)) return FabricUtil.COMPATIBILITY_LATEST;
			SemanticVersion v = (SemanticVersion) min;
			int minor = v.getVersionComponent(0) > 0 ? Integer.MAX_VALUE : v.getVersionComponent(1);
			if (minor < 10) return FabricUtil.COMPATIBILITY_0_9_2;
			if (minor < 14) return FabricUtil.COMPATIBILITY_0_10_0;
			if (minor < 16) return FabricUtil.COMPATIBILITY_0_14_0;
			if (minor < 17) return FabricUtil.COMPATIBILITY_0_16_5;
		}
		return FabricUtil.COMPATIBILITY_LATEST;
	}

	private static List<Path> readClasspath(Path file) throws IOException {
		List<Path> out = new ArrayList<>();
		for (String line : Files.readAllLines(file, StandardCharsets.UTF_8)) {
			line = line.trim();
			if (!line.isEmpty()) out.add(Path.of(line));
		}
		return out;
	}

	private static String required(String key) {
		String v = System.getProperty(key);
		if (v == null || v.isEmpty()) throw new IllegalStateException("Chunky needs the " + key + " setting (start it from Java4U)");
		return v;
	}

	static boolean isSemantic(String v) {
		return Versions.parseLenient(v) instanceof SemanticVersion;
	}
}
