package dev.java4u.chunky.mod;

import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.TreeMap;
import java.util.regex.Pattern;

import net.fabricmc.loader.api.Version;
import net.fabricmc.loader.api.VersionParsingException;
import net.fabricmc.loader.api.metadata.ContactInformation;
import net.fabricmc.loader.api.metadata.CustomValue;
import net.fabricmc.loader.api.metadata.ModDependency;
import net.fabricmc.loader.api.metadata.ModEnvironment;
import net.fabricmc.loader.api.metadata.ModMetadata;
import net.fabricmc.loader.api.metadata.Person;
import net.fabricmc.loader.api.metadata.version.VersionInterval;
import net.fabricmc.loader.api.metadata.version.VersionPredicate;
import dev.java4u.chunky.util.Json;
import dev.java4u.chunky.version.Versions;

/**
 * Mod metadata read from chunky.mod.json or fabric.mod.json (schema version 1; both files
 * use the same layout).
 */
public final class ModMeta implements ModMetadata {
	private static final Pattern ID = Pattern.compile("[a-z][a-z0-9_-]{1,63}");

	/** An entrypoint declaration: an optional language adapter and the value string. */
	public static final class Entry {
		public final String adapter;
		public final String value;

		Entry(String adapter, String value) {
			this.adapter = adapter;
			this.value = value;
		}
	}

	/** A mixin config file and the side it applies to. */
	public static final class MixinConfig {
		public final String config;
		public final ModEnvironment environment;

		MixinConfig(String config, ModEnvironment environment) {
			this.config = config;
			this.environment = environment;
		}
	}

	String type = "fabric";
	String id;
	Version version;
	String name;
	String description = "";
	ModEnvironment environment = ModEnvironment.UNIVERSAL;
	final List<String> provides = new ArrayList<>();
	final List<ModDependency> dependencies = new ArrayList<>();
	final List<Person> authors = new ArrayList<>();
	final List<Person> contributors = new ArrayList<>();
	ContactInformation contact = ContactInformation.EMPTY;
	final List<String> license = new ArrayList<>();
	final TreeMap<Integer, String> icons = new TreeMap<>();
	final Map<String, CustomValue> custom = new LinkedHashMap<>();
	public final Map<String, List<Entry>> entrypoints = new LinkedHashMap<>();
	public final List<String> nestedJars = new ArrayList<>();
	public final List<MixinConfig> mixins = new ArrayList<>();
	public final Map<String, String> languageAdapters = new LinkedHashMap<>();
	public String accessWidener;

	/** A built-in mod such as "minecraft" or "java". */
	public static ModMeta builtin(String id, String name, String version, String description) {
		ModMeta m = new ModMeta();
		m.type = "builtin";
		m.id = id;
		m.name = name;
		m.version = Versions.parseLenient(version);
		m.description = description;
		return m;
	}

	public static ModMeta parse(String text, String source) {
		Map<String, Object> o = Json.obj(Json.parse(text));
		if (o == null) throw new IllegalArgumentException(source + ": metadata must be a JSON object");
		Object schema = o.get("schemaVersion");
		if (schema != null && !(schema instanceof Number && ((Number) schema).intValue() == 1)) {
			throw new IllegalArgumentException(source + ": unsupported schemaVersion " + schema);
		}
		ModMeta m = new ModMeta();
		m.id = Json.str(o.get("id"));
		if (m.id == null || !ID.matcher(m.id).matches()) {
			throw new IllegalArgumentException(source + ": invalid mod id '" + m.id + "'");
		}
		String v = Json.str(o.get("version"));
		if (v == null || v.isEmpty()) throw new IllegalArgumentException(source + ": missing version");
		m.version = Versions.parseLenient(v);
		m.name = Json.str(o.get("name"));
		if (m.name == null || m.name.isEmpty()) m.name = m.id;
		if (Json.str(o.get("description")) != null) m.description = Json.str(o.get("description"));
		m.environment = parseEnvironment(o.get("environment"));
		m.provides.addAll(Json.strings(o.get("provides")));
		m.authors.addAll(people(o.get("authors")));
		m.contributors.addAll(people(o.get("contributors")));
		m.contact = contact(o.get("contact"));
		m.license.addAll(Json.strings(o.get("license")));

		Object icon = o.get("icon");
		if (icon instanceof String) {
			m.icons.put(0, (String) icon);
		} else if (Json.obj(icon) != null) {
			for (Map.Entry<String, Object> e : Json.obj(icon).entrySet()) {
				try {
					m.icons.put(Integer.parseInt(e.getKey()), Json.str(e.getValue()));
				} catch (NumberFormatException ignored) {
				}
			}
		}

		Map<String, Object> eps = Json.obj(o.get("entrypoints"));
		if (eps != null) {
			for (Map.Entry<String, Object> e : eps.entrySet()) {
				List<Entry> list = new ArrayList<>();
				for (Object item : listOf(e.getValue())) {
					if (item instanceof String) {
						list.add(new Entry("default", (String) item));
					} else if (Json.obj(item) != null) {
						Map<String, Object> io = Json.obj(item);
						String adapter = Json.str(io.get("adapter"));
						list.add(new Entry(adapter == null ? "default" : adapter, Json.str(io.get("value"))));
					}
				}
				m.entrypoints.put(e.getKey(), list);
			}
		}

		for (Object jar : listOf(o.get("jars"))) {
			String file = Json.obj(jar) != null ? Json.str(Json.obj(jar).get("file")) : null;
			if (file != null) m.nestedJars.add(file);
		}

		for (Object mx : listOf(o.get("mixins"))) {
			if (mx instanceof String) {
				m.mixins.add(new MixinConfig((String) mx, ModEnvironment.UNIVERSAL));
			} else if (Json.obj(mx) != null) {
				String cfg = Json.str(Json.obj(mx).get("config"));
				if (cfg != null) m.mixins.add(new MixinConfig(cfg, parseEnvironment(Json.obj(mx).get("environment"))));
			}
		}

		Map<String, Object> adapters = Json.obj(o.get("languageAdapters"));
		if (adapters != null) {
			for (Map.Entry<String, Object> e : adapters.entrySet()) {
				if (Json.str(e.getValue()) != null) m.languageAdapters.put(e.getKey(), Json.str(e.getValue()));
			}
		}
		m.accessWidener = Json.str(o.get("accessWidener"));
		if (m.accessWidener == null) m.accessWidener = Json.str(o.get("classTweaker"));

		for (ModDependency.Kind kind : ModDependency.Kind.values()) {
			Map<String, Object> deps = Json.obj(o.get(kind.getKey()));
			if (deps == null) continue;
			for (Map.Entry<String, Object> e : deps.entrySet()) {
				m.dependencies.add(new Dependency(kind, e.getKey(), Json.strings(e.getValue()), source));
			}
		}

		Map<String, Object> custom = Json.obj(o.get("custom"));
		if (custom != null) {
			for (Map.Entry<String, Object> e : custom.entrySet()) m.custom.put(e.getKey(), CustomValues.of(e.getValue()));
		}
		return m;
	}

	private static List<Object> listOf(Object o) {
		if (o == null) return Collections.emptyList();
		if (o instanceof List) return Json.arr(o);
		return Collections.singletonList(o);
	}

	private static ModEnvironment parseEnvironment(Object o) {
		String s = Json.str(o);
		if ("client".equals(s)) return ModEnvironment.CLIENT;
		if ("server".equals(s)) return ModEnvironment.SERVER;
		return ModEnvironment.UNIVERSAL;
	}

	private static List<Person> people(Object o) {
		List<Person> out = new ArrayList<>();
		for (Object p : listOf(o)) {
			if (p instanceof String) {
				out.add(person((String) p, ContactInformation.EMPTY));
			} else if (Json.obj(p) != null) {
				String n = Json.str(Json.obj(p).get("name"));
				if (n != null) out.add(person(n, contact(Json.obj(p).get("contact"))));
			}
		}
		return out;
	}

	private static Person person(String name, ContactInformation contact) {
		return new Person() {
			@Override
			public String getName() {
				return name;
			}

			@Override
			public ContactInformation getContact() {
				return contact;
			}

			@Override
			public String toString() {
				return name;
			}
		};
	}

	private static ContactInformation contact(Object o) {
		Map<String, Object> m = Json.obj(o);
		if (m == null || m.isEmpty()) return ContactInformation.EMPTY;
		Map<String, String> values = new LinkedHashMap<>();
		for (Map.Entry<String, Object> e : m.entrySet()) {
			if (Json.str(e.getValue()) != null) values.put(e.getKey(), Json.str(e.getValue()));
		}
		Map<String, String> view = Collections.unmodifiableMap(values);
		return new ContactInformation() {
			@Override
			public Optional<String> get(String key) {
				return Optional.ofNullable(view.get(key));
			}

			@Override
			public Map<String, String> asMap() {
				return view;
			}
		};
	}

	// ---------------------------------------------------------------- ModMetadata

	@Override
	public String getType() {
		return type;
	}

	@Override
	public String getId() {
		return id;
	}

	@Override
	public Collection<String> getProvides() {
		return Collections.unmodifiableList(provides);
	}

	@Override
	public Version getVersion() {
		return version;
	}

	@Override
	public ModEnvironment getEnvironment() {
		return environment;
	}

	@Override
	public Collection<ModDependency> getDependencies() {
		return Collections.unmodifiableList(dependencies);
	}

	@Override
	public String getName() {
		return name;
	}

	@Override
	public String getDescription() {
		return description;
	}

	@Override
	public Collection<Person> getAuthors() {
		return Collections.unmodifiableList(authors);
	}

	@Override
	public Collection<Person> getContributors() {
		return Collections.unmodifiableList(contributors);
	}

	@Override
	public ContactInformation getContact() {
		return contact;
	}

	@Override
	public Collection<String> getLicense() {
		return Collections.unmodifiableList(license);
	}

	@Override
	public Optional<String> getIconPath(int size) {
		if (icons.isEmpty()) return Optional.empty();
		Map.Entry<Integer, String> e = icons.ceilingEntry(size);
		if (e == null) e = icons.lastEntry();
		return Optional.ofNullable(e.getValue());
	}

	@Override
	public boolean containsCustomElement(String key) {
		return custom.containsKey(key);
	}

	@Override
	public boolean containsCustomValue(String key) {
		return custom.containsKey(key);
	}

	@Override
	public CustomValue getCustomValue(String key) {
		return custom.get(key);
	}

	@Override
	public Map<String, CustomValue> getCustomValues() {
		return Collections.unmodifiableMap(custom);
	}

	@Override
	public String toString() {
		return id + " " + version.getFriendlyString();
	}

	/** One dependency entry. An unparsable range never matches, so the problem gets reported. */
	static final class Dependency implements ModDependency {
		private final Kind kind;
		private final String modId;
		private final List<VersionPredicate> predicates = new ArrayList<>();
		private final String raw;

		Dependency(Kind kind, String modId, List<String> ranges, String source) {
			this.kind = kind;
			this.modId = modId;
			this.raw = String.join(" || ", ranges.isEmpty() ? List.of("*") : ranges);
			try {
				predicates.addAll(VersionPredicate.parse(ranges.isEmpty() ? List.of("*") : ranges));
			} catch (VersionParsingException e) {
				dev.java4u.chunky.Log.warn(source + ": can't read version range '" + raw + "' for " + modId + ": " + e.getMessage());
			}
		}

		@Override
		public Kind getKind() {
			return kind;
		}

		@Override
		public String getModId() {
			return modId;
		}

		@Override
		public boolean matches(Version version) {
			for (VersionPredicate p : predicates) {
				if (p.test(version)) return true;
			}
			return false;
		}

		@Override
		public Collection<VersionPredicate> getVersionRequirements() {
			return Collections.unmodifiableList(predicates);
		}

		@Override
		public List<VersionInterval> getVersionIntervals() {
			List<VersionInterval> out = new ArrayList<>();
			for (VersionPredicate p : predicates) out = VersionInterval.or(out, p.getInterval());
			return out;
		}

		@Override
		public String toString() {
			return modId + " " + raw;
		}
	}
}
