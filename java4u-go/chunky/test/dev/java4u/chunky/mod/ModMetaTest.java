package dev.java4u.chunky.mod;

import static dev.java4u.chunky.test.Check.*;

import java.util.ArrayList;
import java.util.List;

import dev.java4u.chunky.version.Versions;
import net.fabricmc.loader.api.SemanticVersion;
import net.fabricmc.loader.api.metadata.ModDependency;
import net.fabricmc.loader.api.metadata.ModEnvironment;

public final class ModMetaTest {
	private ModMetaTest() {
	}

	static final String FULL = """
			{
			  // Fabric allows comments in some mods' metadata.
			  "schemaVersion": 1,
			  "id": "example-mod",
			  "version": "2.4.1+26.3",
			  "environment": "client",
			  "provides": ["example"],
			  "authors": ["Alex", {"name": "Sam", "contact": {"homepage": "https://example.com"}}],
			  "license": "MIT",
			  "icon": {"16": "small.png", "64": "big.png"},
			  "entrypoints": {
			    "main": ["com.example.Main"],
			    "client": [{"adapter": "kotlin", "value": "com.example.Client"}]
			  },
			  "jars": [{"file": "META-INF/jars/lib.jar"}],
			  "mixins": ["example.mixins.json", {"config": "example.client.mixins.json", "environment": "client"}],
			  "accessWidener": "example.accesswidener",
			  "depends": {"fabricloader": ">=0.15", "minecraft": ["~26.3", "26.4.x"]},
			  "breaks": {"badmod": "<3"},
			  "custom": {"modmenu": {"badges": ["library"]}}
			}
			""";

	public static void testParsesFabricMetadata() {
		ModMeta m = ModMeta.parse(FULL, "test");
		eq("example-mod", m.getId(), "id");
		eq("example-mod", m.getName(), "name falls back to the id");
		isTrue(m.getVersion() instanceof SemanticVersion, "semantic version");
		eq("2.4.1+26.3", m.getVersion().getFriendlyString(), "version text");
		eq(ModEnvironment.CLIENT, m.getEnvironment(), "environment");
		eq(List.of("example"), new ArrayList<>(m.getProvides()), "provides");
		eq(2, m.getAuthors().size(), "authors");
		eq(List.of("MIT"), new ArrayList<>(m.getLicense()), "license");
		eq("big.png", m.getIconPath(64).orElse(null), "large icon");

		eq("com.example.Main", m.entrypoints.get("main").get(0).value, "main entrypoint");
		eq("default", m.entrypoints.get("main").get(0).adapter, "default adapter");
		eq("kotlin", m.entrypoints.get("client").get(0).adapter, "kotlin adapter");
		eq(List.of("META-INF/jars/lib.jar"), m.nestedJars, "nested jars");
		eq(2, m.mixins.size(), "mixin configs");
		eq(ModEnvironment.UNIVERSAL, m.mixins.get(0).environment, "plain mixin config is universal");
		eq(ModEnvironment.CLIENT, m.mixins.get(1).environment, "client mixin config");
		eq("example.accesswidener", m.accessWidener, "access widener");
		isTrue(m.containsCustomValue("modmenu"), "custom value");
	}

	public static void testDependencies() {
		ModMeta m = ModMeta.parse(FULL, "test");
		ModDependency minecraft = null, breaks = null;
		for (ModDependency d : m.getDependencies()) {
			if (d.getModId().equals("minecraft")) minecraft = d;
			if (d.getModId().equals("badmod")) breaks = d;
		}
		isTrue(minecraft != null && minecraft.getKind() == ModDependency.Kind.DEPENDS, "minecraft dependency");
		isTrue(minecraft.matches(Versions.parseLenient("26.3.2")), "~26.3 has 26.3.2");
		isTrue(minecraft.matches(Versions.parseLenient("26.4.1")), "26.4.x has 26.4.1");
		isFalse(minecraft.matches(Versions.parseLenient("26.5")), "neither range has 26.5");
		isTrue(breaks != null && breaks.getKind() == ModDependency.Kind.BREAKS, "breaks entry");
		isTrue(breaks.matches(Versions.parseLenient("2.9")), "<3 has 2.9");
	}

	public static void testMinimalMetadata() {
		ModMeta m = ModMeta.parse("{\"id\": \"tiny\", \"version\": \"1.0_beta\"}", "test");
		eq(ModEnvironment.UNIVERSAL, m.getEnvironment(), "default environment");
		isFalse(m.getVersion() instanceof SemanticVersion, "odd version is kept as text");
		eq(0, m.getDependencies().size(), "no dependencies");
	}

	public static void testRejectsBadMetadata() {
		IllegalArgumentException e = fails(IllegalArgumentException.class,
				() -> ModMeta.parse("{\"id\": \"Bad Id\", \"version\": \"1\"}", "bad.json"), "bad id");
		contains(e.getMessage(), "bad.json", "error names the file");
		fails(IllegalArgumentException.class, () -> ModMeta.parse("{\"id\": \"x\", \"version\": \"1\"}", "t"), "one-letter id");
		fails(IllegalArgumentException.class, () -> ModMeta.parse("{\"id\": \"good\"}", "t"), "missing version");
		fails(IllegalArgumentException.class,
				() -> ModMeta.parse("{\"schemaVersion\": 2, \"id\": \"good\", \"version\": \"1\"}", "t"), "schema 2");
		fails(IllegalArgumentException.class, () -> ModMeta.parse("[]", "t"), "not an object");
	}
}
