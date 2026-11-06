package dev.java4u.chunky;

import java.io.File;
import java.io.IOException;
import java.io.UncheckedIOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.function.BiConsumer;
import java.util.function.Consumer;

import net.fabricmc.api.EnvType;
import net.fabricmc.loader.api.EntrypointException;
import net.fabricmc.loader.api.FabricLoader;
import net.fabricmc.loader.api.LanguageAdapter;
import net.fabricmc.loader.api.LanguageAdapterException;
import net.fabricmc.loader.api.MappingResolver;
import net.fabricmc.loader.api.ModContainer;
import net.fabricmc.loader.api.ObjectShare;
import net.fabricmc.loader.api.entrypoint.EntrypointContainer;
import dev.java4u.chunky.loader.ChunkyClassLoader;
import dev.java4u.chunky.mod.ChunkyMod;
import dev.java4u.chunky.mod.ModMeta;

/** Chunky's implementation of the loader API that Fabric mods use. */
public final class ChunkyLoader implements FabricLoader {
	private static volatile ChunkyLoader instance;

	public static ChunkyLoader get() {
		ChunkyLoader l = instance;
		if (l == null) throw new IllegalStateException("Chunky hasn't started yet");
		return l;
	}

	static ChunkyLoader init(List<ChunkyMod> mods, Path gameDir, String gameVersion, String[] args, ChunkyClassLoader cl) {
		instance = new ChunkyLoader(mods, gameDir, gameVersion, args, cl);
		return instance;
	}

	private final List<ChunkyMod> mods;
	private final Map<String, ChunkyMod> byId = new LinkedHashMap<>();
	private final Path gameDir;
	private final String gameVersion;
	private final String[] args;
	final ChunkyClassLoader classLoader;
	private volatile Object gameInstance;
	private final Map<String, Object> entrypointCache = new ConcurrentHashMap<>();
	private final Map<String, LanguageAdapter> adapters = new ConcurrentHashMap<>();
	private final Share share = new Share();

	private ChunkyLoader(List<ChunkyMod> mods, Path gameDir, String gameVersion, String[] args, ChunkyClassLoader cl) {
		this.mods = Collections.unmodifiableList(new ArrayList<>(mods));
		this.gameDir = gameDir;
		this.gameVersion = gameVersion;
		this.args = args.clone();
		this.classLoader = cl;
		for (ChunkyMod m : mods) byId.put(m.id(), m);
		for (ChunkyMod m : mods) {
			for (String alias : m.meta.getProvides()) byId.putIfAbsent(alias, m);
		}
	}

	public List<ChunkyMod> mods() {
		return mods;
	}

	void setGameInstance(Object o) {
		gameInstance = o;
	}

	// ---------------------------------------------------------------- entrypoints

	private static final class Container<T> implements EntrypointContainer<T> {
		private final T value;
		private final ModContainer provider;
		private final String definition;

		Container(T value, ModContainer provider, String definition) {
			this.value = value;
			this.provider = provider;
			this.definition = definition;
		}

		@Override
		public T getEntrypoint() {
			return value;
		}

		@Override
		public ModContainer getProvider() {
			return provider;
		}

		@Override
		public String getDefinition() {
			return definition;
		}
	}

	@Override
	public <T> List<EntrypointContainer<T>> getEntrypointContainers(String key, Class<T> type) {
		List<EntrypointContainer<T>> out = new ArrayList<>();
		EntrypointException failure = null;
		for (ChunkyMod mod : mods) {
			List<ModMeta.Entry> entries = mod.meta.entrypoints.get(key);
			if (entries == null) continue;
			for (ModMeta.Entry e : entries) {
				if (e.value == null) continue;
				try {
					out.add(new Container<>(create(mod, e, type), mod, e.value));
				} catch (Throwable t) {
					EntrypointException ex = new EntrypointException(key, mod.id(), t);
					if (failure == null) failure = ex;
					else failure.addSuppressed(ex);
				}
			}
		}
		if (failure != null) throw failure;
		return out;
	}

	@Override
	public <T> List<T> getEntrypoints(String key, Class<T> type) {
		List<T> out = new ArrayList<>();
		for (EntrypointContainer<T> c : getEntrypointContainers(key, type)) out.add(c.getEntrypoint());
		return out;
	}

	@Override
	public <T> void invokeEntrypoints(String key, Class<T> type, Consumer<? super T> invoker) {
		RuntimeException failure = null;
		for (EntrypointContainer<T> c : getEntrypointContainers(key, type)) {
			try {
				invoker.accept(c.getEntrypoint());
			} catch (Throwable t) {
				EntrypointException ex = new EntrypointException(key, c.getProvider().getMetadata().getId(), t);
				if (failure == null) failure = ex;
				else failure.addSuppressed(ex);
			}
		}
		if (failure != null) throw failure;
	}

	/** One object per entrypoint definition, shared between keys like Fabric does. */
	private <T> T create(ChunkyMod mod, ModMeta.Entry e, Class<T> type) throws LanguageAdapterException {
		String cacheKey = mod.id() + "\u0000" + e.adapter + "\u0000" + e.value;
		Object o = entrypointCache.get(cacheKey);
		if (o == null) {
			o = adapter(e.adapter).create(mod, e.value, type);
			Object prev = entrypointCache.putIfAbsent(cacheKey, o);
			if (prev != null) o = prev;
		}
		if (!type.isInstance(o)) {
			throw new LanguageAdapterException(e.value + " in " + mod.id() + " is not a " + type.getName());
		}
		return type.cast(o);
	}

	private LanguageAdapter adapter(String name) throws LanguageAdapterException {
		if (name == null || name.equals("default")) return DefaultLanguageAdapter.INSTANCE;
		LanguageAdapter a = adapters.get(name);
		if (a != null) return a;
		for (ChunkyMod m : mods) {
			String cls = m.meta.languageAdapters.get(name);
			if (cls == null) continue;
			try {
				a = (LanguageAdapter) Class.forName(cls, true, classLoader).getDeclaredConstructor().newInstance();
			} catch (ReflectiveOperationException | ClassCastException ex) {
				throw new LanguageAdapterException("can't create language adapter " + name + " from " + m.id(), ex);
			}
			adapters.put(name, a);
			return a;
		}
		throw new LanguageAdapterException("no mod provides the language adapter '" + name + "'");
	}

	// ---------------------------------------------------------------- mods

	@Override
	public Optional<ModContainer> getModContainer(String id) {
		return Optional.ofNullable(byId.get(id));
	}

	@Override
	public Collection<ModContainer> getAllMods() {
		return Collections.unmodifiableList(mods);
	}

	@Override
	public boolean isModLoaded(String id) {
		return byId.containsKey(id);
	}

	// ---------------------------------------------------------------- environment

	@Override
	public ObjectShare getObjectShare() {
		return share;
	}

	@Override
	public MappingResolver getMappingResolver() {
		return IdentityMappings.INSTANCE;
	}

	@Override
	public boolean isDevelopmentEnvironment() {
		return Boolean.getBoolean("chunky.development");
	}

	@Override
	public EnvType getEnvironmentType() {
		return EnvType.CLIENT;
	}

	@Override
	public String getRawGameVersion() {
		return gameVersion;
	}

	@Override
	public Object getGameInstance() {
		return gameInstance;
	}

	@Override
	public Path getGameDir() {
		return gameDir;
	}

	@Override
	@Deprecated
	public File getGameDirectory() {
		return gameDir.toFile();
	}

	@Override
	public Path getConfigDir() {
		Path dir = gameDir.resolve("config");
		try {
			Files.createDirectories(dir);
		} catch (IOException e) {
			throw new UncheckedIOException("can't create " + dir, e);
		}
		return dir;
	}

	@Override
	@Deprecated
	public File getConfigDirectory() {
		return getConfigDir().toFile();
	}

	@Override
	public String[] getLaunchArguments(boolean sanitize) {
		if (!sanitize) return args.clone();
		List<String> out = new ArrayList<>();
		for (int i = 0; i < args.length; i++) {
			String a = args[i];
			if ((a.equals("--accessToken") || a.equals("--session") || a.equals("--xuid") || a.equals("--clientId"))
					&& i + 1 < args.length) {
				i++;
				continue;
			}
			out.add(a);
		}
		return out.toArray(new String[0]);
	}

	/** Mods share objects by key; listeners fire as soon as a value appears. */
	private static final class Share implements ObjectShare {
		private final Map<String, Object> values = new ConcurrentHashMap<>();
		private final Map<String, List<BiConsumer<String, Object>>> waiting = new ConcurrentHashMap<>();

		@Override
		public Object get(String key) {
			return values.get(check(key));
		}

		@Override
		public void whenAvailable(String key, BiConsumer<String, Object> consumer) {
			check(key);
			Object v;
			synchronized (this) {
				v = values.get(key);
				if (v == null) {
					waiting.computeIfAbsent(key, k -> new CopyOnWriteArrayList<>()).add(consumer);
					return;
				}
			}
			consumer.accept(key, v);
		}

		@Override
		public Object put(String key, Object value) {
			check(key);
			if (value == null) throw new NullPointerException("value");
			Object prev;
			List<BiConsumer<String, Object>> listeners;
			synchronized (this) {
				prev = values.put(key, value);
				listeners = waiting.remove(key);
			}
			if (listeners != null) for (BiConsumer<String, Object> l : listeners) l.accept(key, value);
			return prev;
		}

		@Override
		public Object putIfAbsent(String key, Object value) {
			check(key);
			synchronized (this) {
				Object prev = values.get(key);
				if (prev != null) return prev;
			}
			put(key, value);
			return null;
		}

		@Override
		public Object remove(String key) {
			return values.remove(check(key));
		}

		private static String check(String key) {
			if (key == null || key.indexOf(':') <= 0) {
				throw new IllegalArgumentException("ObjectShare keys look like modid:name, got " + key);
			}
			return key;
		}
	}

	/** 26.x and newer have real names everywhere, so every name maps to itself. */
	private enum IdentityMappings implements MappingResolver {
		INSTANCE;

		@Override
		public Collection<String> getNamespaces() {
			return List.of("official", "named", "intermediary");
		}

		@Override
		public String getCurrentRuntimeNamespace() {
			return "official";
		}

		@Override
		public String mapClassName(String namespace, String className) {
			return className;
		}

		@Override
		public String unmapClassName(String targetNamespace, String className) {
			return className;
		}

		@Override
		public String mapFieldName(String namespace, String owner, String name, String descriptor) {
			return name;
		}

		@Override
		public String mapMethodName(String namespace, String owner, String name, String descriptor) {
			return name;
		}
	}
}
