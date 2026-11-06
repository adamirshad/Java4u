package dev.java4u.chunky.loader;

import java.io.IOException;
import java.io.InputStream;
import java.io.UncheckedIOException;
import java.net.MalformedURLException;
import java.net.URL;
import java.nio.file.Path;
import java.security.CodeSource;
import java.security.ProtectionDomain;
import java.security.SecureClassLoader;
import java.security.cert.Certificate;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Enumeration;
import java.util.List;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;
import java.util.jar.JarFile;
import java.util.zip.ZipEntry;

/**
 * Loads the game, its libraries and every mod, patching classes as they are first used.
 *
 * Chunky itself, Mixin, ASM and the Fabric API types live in the parent loader so the
 * game and all mods share one copy. Everything else is loaded here, child-first.
 * Jars stay open and nothing is cached in memory: bytes are read only when a class is
 * first requested and dropped once it's defined.
 */
public final class ChunkyClassLoader extends SecureClassLoader {
	static {
		registerAsParallelCapable();
	}

	/** Packages always taken from the parent so there is exactly one copy of them. */
	private static final String[] PARENT_ONLY = {
			"java.", "jdk.", "sun.",
			"dev.java4u.chunky.", "org.spongepowered.", "org.objectweb.asm.", "com.llamalad7.mixinextras.",
			"net.fabricmc.api.", "net.fabricmc.loader.api.",
	};

	/**
	 * Mostly JDK packages that some game libraries also use (JNA lives in com.sun.jna,
	 * jsr305 in javax.annotation): ask the parent first, then the game's jars.
	 */
	private static final String[] PARENT_FIRST = {"javax.", "com.sun."};

	private static final class Source {
		final Path path;
		final JarFile jar;
		final ProtectionDomain domain;
		final String urlPrefix;

		Source(Path path, ClassLoader loader) throws IOException {
			this.path = path;
			this.jar = new JarFile(path.toFile(), false);
			URL url = path.toUri().toURL();
			this.domain = new ProtectionDomain(new CodeSource(url, (Certificate[]) null), null, loader, null);
			this.urlPrefix = "jar:" + url + "!/";
		}
	}

	private final List<Source> sources = new ArrayList<>();
	private final ClassLoader parent;
	private final Set<String> invalid = ConcurrentHashMap.newKeySet();
	private volatile Transformer transformer = Transformer.NONE;

	public ChunkyClassLoader(List<Path> classpath, ClassLoader parent) {
		super("chunky", parent);
		this.parent = parent;
		for (Path p : classpath) {
			try {
				sources.add(new Source(p, this));
			} catch (IOException e) {
				throw new UncheckedIOException("can't open " + p, e);
			}
		}
	}

	public void setTransformer(Transformer t) {
		this.transformer = t;
	}

	static boolean parentOnly(String name) {
		for (String p : PARENT_ONLY) {
			if (name.startsWith(p)) return true;
		}
		return false;
	}

	private static boolean parentFirst(String name) {
		for (String p : PARENT_FIRST) {
			if (name.startsWith(p)) return true;
		}
		return false;
	}

	@Override
	protected Class<?> loadClass(String name, boolean resolve) throws ClassNotFoundException {
		synchronized (getClassLoadingLock(name)) {
			Class<?> c = findLoadedClass(name);
			if (c == null && parentFirst(name)) {
				try {
					c = parent.loadClass(name);
				} catch (ClassNotFoundException notInJdk) {
					c = defineFromSources(name);
				}
			} else if (c == null && !parentOnly(name)) {
				if (invalid.contains(name)) throw new ClassNotFoundException(name + " was marked invalid by Mixin");
				c = defineFromSources(name);
			}
			if (c == null) c = parent.loadClass(name);
			if (resolve) resolveClass(c);
			return c;
		}
	}

	@Override
	protected Class<?> findClass(String name) throws ClassNotFoundException {
		Class<?> c = parentOnly(name) ? null : defineFromSources(name);
		if (c == null) throw new ClassNotFoundException(name);
		return c;
	}

	private Class<?> defineFromSources(String name) throws ClassNotFoundException {
		String entry = name.replace('.', '/') + ".class";
		for (Source s : sources) {
			ZipEntry e = s.jar.getEntry(entry);
			if (e == null) continue;
			byte[] raw = read(s, e);
			byte[] bytes = transformer.transform(name, raw);
			definePackageFor(name, s);
			return defineClass(name, bytes, 0, bytes.length, s.domain);
		}
		// Mixin can generate helper classes that exist in no jar.
		byte[] generated = transformer.generate(name);
		if (generated != null) {
			definePackageFor(name, null);
			return defineClass(name, generated, 0, generated.length, (ProtectionDomain) null);
		}
		return null;
	}

	private static byte[] read(Source s, ZipEntry e) throws ClassNotFoundException {
		try (InputStream in = s.jar.getInputStream(e)) {
			return in.readAllBytes();
		} catch (IOException ex) {
			throw new ClassNotFoundException("can't read " + e.getName() + " from " + s.path, ex);
		}
	}

	private void definePackageFor(String className, Source s) {
		int dot = className.lastIndexOf('.');
		if (dot < 0) return;
		String pkg = className.substring(0, dot);
		if (getDefinedPackage(pkg) != null) return;
		try {
			definePackage(pkg, null, null, null, null, null, null, null);
		} catch (IllegalArgumentException alreadyDefined) {
			// Another thread defined it first.
		}
	}

	/**
	 * Class bytes as Mixin should see them: from the game or a mod with Chunky's own patches
	 * applied (but not Mixin's), or the original bytes for JDK and parent classes.
	 */
	public byte[] getPreMixinBytes(String name) throws IOException {
		String entry = name.replace('.', '/') + ".class";
		if (!parentOnly(name)) {
			for (Source s : sources) {
				ZipEntry e = s.jar.getEntry(entry);
				if (e == null) continue;
				try (InputStream in = s.jar.getInputStream(e)) {
					return transformer.preMixin(name, in.readAllBytes());
				}
			}
		}
		try (InputStream in = parent.getResourceAsStream(entry)) {
			if (in != null) return in.readAllBytes();
		}
		try (InputStream in = ClassLoader.getSystemResourceAsStream(entry)) {
			return in == null ? null : in.readAllBytes();
		}
	}

	public boolean isLoaded(String name) {
		return findLoadedClass(name) != null;
	}

	public void markInvalid(String name) {
		invalid.add(name);
	}

	// ---------------------------------------------------------------- resources (child-first)

	@Override
	public URL getResource(String name) {
		URL u = findResource(name);
		return u != null ? u : parent.getResource(name);
	}

	@Override
	protected URL findResource(String name) {
		String n = name.startsWith("/") ? name.substring(1) : name;
		for (Source s : sources) {
			if (s.jar.getEntry(n) != null) return url(s, n);
		}
		return null;
	}

	@Override
	public Enumeration<URL> getResources(String name) throws IOException {
		List<URL> out = new ArrayList<>(Collections.list(findResources(name)));
		out.addAll(Collections.list(parent.getResources(name)));
		return Collections.enumeration(out);
	}

	@Override
	protected Enumeration<URL> findResources(String name) {
		String n = name.startsWith("/") ? name.substring(1) : name;
		List<URL> out = new ArrayList<>();
		for (Source s : sources) {
			if (s.jar.getEntry(n) != null) out.add(url(s, n));
		}
		return Collections.enumeration(out);
	}

	@Override
	public InputStream getResourceAsStream(String name) {
		String n = name.startsWith("/") ? name.substring(1) : name;
		for (Source s : sources) {
			ZipEntry e = s.jar.getEntry(n);
			if (e == null) continue;
			try {
				return s.jar.getInputStream(e);
			} catch (IOException ex) {
				return null;
			}
		}
		return parent.getResourceAsStream(name);
	}

	private static URL url(Source s, String entry) {
		try {
			return java.net.URI.create(s.urlPrefix + entry.replace(" ", "%20")).toURL();
		} catch (MalformedURLException | IllegalArgumentException e) {
			throw new IllegalStateException("bad resource name " + entry, e);
		}
	}
}
