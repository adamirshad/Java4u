package dev.java4u.chunky.mod;

import static dev.java4u.chunky.test.Check.*;

import java.util.ArrayList;
import java.util.List;

import net.fabricmc.api.EnvType;

public final class ResolverTest {
	private ResolverTest() {
	}

	/** A mod from a one-line fabric.mod.json body (without the braces). */
	private static ChunkyMod mod(String json) {
		return new ChunkyMod(ModMeta.parse("{" + json + "}", "test"), null, null, null);
	}

	private static List<ChunkyMod> builtins() {
		return List.of(new ChunkyMod(ModMeta.builtin("minecraft", "Minecraft", "26.3", "The base game"), null, null, null));
	}

	private static List<String> ids(List<ChunkyMod> mods) {
		List<String> out = new ArrayList<>();
		for (ChunkyMod m : mods) out.add(m.id());
		return out;
	}

	private static List<ChunkyMod> resolve(EnvType env, ChunkyMod... mods) throws Resolver.ResolutionException {
		return Resolver.resolve(builtins(), List.of(mods), env);
	}

	public static void testDependenciesLoadFirst() throws Exception {
		List<ChunkyMod> out = resolve(EnvType.CLIENT,
				mod("\"id\": \"app\", \"version\": \"1\", \"depends\": {\"library\": \">=2\", \"minecraft\": \"~26.3\"}"),
				mod("\"id\": \"library\", \"version\": \"2.1\""));
		eq(List.of("minecraft", "library", "app"), ids(out), "load order");
	}

	public static void testMissingDependency() {
		Resolver.ResolutionException e = fails(Resolver.ResolutionException.class,
				() -> resolve(EnvType.CLIENT, mod("\"id\": \"app\", \"version\": \"1\", \"depends\": {\"fabric-api\": \"*\"}")),
				"missing dependency");
		contains(e.getMessage(), "needs", "message");
		contains(e.getMessage(), "isn't installed", "message");
	}

	public static void testWrongVersion() {
		Resolver.ResolutionException e = fails(Resolver.ResolutionException.class,
				() -> resolve(EnvType.CLIENT, mod("\"id\": \"app\", \"version\": \"1\", \"depends\": {\"minecraft\": \"~26.4\"}")),
				"wrong Minecraft version");
		contains(e.getMessage(), "is installed", "message names the installed version");
	}

	public static void testBreaks() {
		Resolver.ResolutionException e = fails(Resolver.ResolutionException.class,
				() -> resolve(EnvType.CLIENT,
						mod("\"id\": \"app\", \"version\": \"1\", \"breaks\": {\"oldlib\": \"<2\"}"),
						mod("\"id\": \"oldlib\", \"version\": \"1.5\"")),
				"breaks");
		contains(e.getMessage(), "doesn't work with", "message");
	}

	public static void testBreaksOnlyItsRange() throws Exception {
		List<ChunkyMod> out = resolve(EnvType.CLIENT,
				mod("\"id\": \"app\", \"version\": \"1\", \"breaks\": {\"oldlib\": \"<2\"}"),
				mod("\"id\": \"oldlib\", \"version\": \"2.0\""));
		eq(3, out.size(), "newer oldlib is fine");
	}

	public static void testNewestCopyWins() throws Exception {
		List<ChunkyMod> out = resolve(EnvType.CLIENT,
				mod("\"id\": \"lib\", \"version\": \"1.0\""),
				mod("\"id\": \"lib\", \"version\": \"1.2\""),
				mod("\"id\": \"lib\", \"version\": \"1.1\""));
		eq(2, out.size(), "one copy kept");
		eq("1.2", out.get(1).meta.getVersion().getFriendlyString(), "newest copy");
	}

	public static void testBuiltinWins() throws Exception {
		List<ChunkyMod> out = resolve(EnvType.CLIENT, mod("\"id\": \"minecraft\", \"version\": \"99\""));
		eq(1, out.size(), "one minecraft");
		eq("builtin", out.get(0).meta.getType(), "built-in kept");
	}

	public static void testSkipsOtherSide() throws Exception {
		ChunkyMod clientOnly = mod("\"id\": \"shaders\", \"version\": \"1\", \"environment\": \"client\"");
		ChunkyMod serverOnly = mod("\"id\": \"backups\", \"version\": \"1\", \"environment\": \"server\"");
		eq(List.of("minecraft", "shaders"), ids(resolve(EnvType.CLIENT, clientOnly, serverOnly)), "client");
		eq(List.of("minecraft", "backups"), ids(resolve(EnvType.SERVER, clientOnly, serverOnly)), "server");
	}

	public static void testProvides() throws Exception {
		List<ChunkyMod> out = resolve(EnvType.CLIENT,
				mod("\"id\": \"app\", \"version\": \"1\", \"depends\": {\"old-name\": \"*\"}"),
				mod("\"id\": \"new-name\", \"version\": \"3\", \"provides\": [\"old-name\"]"));
		eq(List.of("minecraft", "new-name", "app"), ids(out), "alias satisfies the dependency");
	}

	public static void testCyclesDontHang() throws Exception {
		List<ChunkyMod> out = resolve(EnvType.CLIENT,
				mod("\"id\": \"aa\", \"version\": \"1\", \"depends\": {\"bb\": \"*\"}"),
				mod("\"id\": \"bb\", \"version\": \"1\", \"depends\": {\"aa\": \"*\"}"));
		eq(3, out.size(), "both mods load once");
	}
}
