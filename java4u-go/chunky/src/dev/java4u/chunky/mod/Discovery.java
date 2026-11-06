package dev.java4u.chunky.mod;

import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.DirectoryStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.ArrayList;
import java.util.HexFormat;
import java.util.List;
import java.util.zip.ZipEntry;
import java.util.zip.ZipFile;

import dev.java4u.chunky.Log;

/** Finds mods in the mods folder, including jars nested inside other mods. */
public final class Discovery {
	private static final int MAX_DEPTH = 6;

	private final Path cacheDir;
	private final List<ChunkyMod> found = new ArrayList<>();

	private Discovery(Path cacheDir) {
		this.cacheDir = cacheDir;
	}

	/** Every mod jar (and nested jar) under modsDir. Built-in mods are added by the caller. */
	public static List<ChunkyMod> scan(Path modsDir, Path cacheDir) throws IOException {
		Discovery d = new Discovery(cacheDir);
		if (!Files.isDirectory(modsDir)) return d.found;
		List<Path> jars = new ArrayList<>();
		try (DirectoryStream<Path> ds = Files.newDirectoryStream(modsDir, "*.jar")) {
			for (Path p : ds) if (Files.isRegularFile(p)) jars.add(p);
		}
		jars.sort(null);
		for (Path jar : jars) {
			try {
				ChunkyMod m = d.read(jar, null, null, 0);
				if (m == null) Log.warn(jar.getFileName() + " is not a Chunky or Fabric mod; skipping it.");
			} catch (IOException | RuntimeException e) {
				throw new IOException("Can't read mod " + jar.getFileName() + ": " + e.getMessage(), e);
			}
		}
		return d.found;
	}

	private ChunkyMod read(Path jar, ChunkyMod parent, String subLocation, int depth) throws IOException {
		ModMeta meta;
		List<byte[]> nestedBytes = new ArrayList<>();
		List<String> nestedNames = new ArrayList<>();
		try (ZipFile zip = new ZipFile(jar.toFile())) {
			String text = readEntry(zip, "chunky.mod.json");
			String source = jar.getFileName() + "/chunky.mod.json";
			if (text == null) {
				text = readEntry(zip, "fabric.mod.json");
				source = jar.getFileName() + "/fabric.mod.json";
			}
			if (text == null) {
				if (parent == null) return null;
				// A plain library shipped inside a mod: load it, but it isn't a mod itself.
				meta = ModMeta.builtin(libraryId(subLocation), stripJar(subLocation), "0", "Library bundled with " + parent.id());
				meta.type = "library";
			} else {
				meta = ModMeta.parse(text, source);
				for (String nested : meta.nestedJars) {
					ZipEntry e = zip.getEntry(nested);
					if (e == null) {
						Log.warn(meta.getId() + " lists missing nested jar " + nested);
						continue;
					}
					try (InputStream in = zip.getInputStream(e)) {
						nestedBytes.add(in.readAllBytes());
						nestedNames.add(nested);
					}
				}
			}
		}
		ChunkyMod mod = new ChunkyMod(meta, jar, parent, subLocation);
		found.add(mod);
		if (depth >= MAX_DEPTH && !nestedBytes.isEmpty()) {
			Log.warn(meta.getId() + ": nested jars are too deep; ignoring the rest");
			return mod;
		}
		for (int i = 0; i < nestedBytes.size(); i++) {
			Path cached = cache(nestedNames.get(i), nestedBytes.get(i));
			read(cached, mod, nestedNames.get(i), depth + 1);
		}
		return mod;
	}

	/** Writes a nested jar to the cache once, named by its hash so updates never collide. */
	private Path cache(String name, byte[] data) throws IOException {
		String base = stripJar(name.substring(name.lastIndexOf('/') + 1)).replaceAll("[^A-Za-z0-9._+-]", "_");
		Path out = cacheDir.resolve(sha1(data).substring(0, 16) + "-" + base + ".jar");
		if (!Files.exists(out) || Files.size(out) != data.length) {
			Files.createDirectories(cacheDir);
			Path tmp = Files.createTempFile(cacheDir, "nested", ".tmp");
			Files.write(tmp, data);
			Files.move(tmp, out, StandardCopyOption.REPLACE_EXISTING, StandardCopyOption.ATOMIC_MOVE);
		}
		return out;
	}

	private static String readEntry(ZipFile zip, String name) throws IOException {
		ZipEntry e = zip.getEntry(name);
		if (e == null) return null;
		try (InputStream in = zip.getInputStream(e)) {
			return new String(in.readAllBytes(), StandardCharsets.UTF_8);
		}
	}

	private static String stripJar(String s) {
		String n = s.substring(s.lastIndexOf('/') + 1);
		return n.endsWith(".jar") ? n.substring(0, n.length() - 4) : n;
	}

	private static String libraryId(String sub) {
		String id = stripJar(sub).toLowerCase().replaceAll("[^a-z0-9_-]", "_");
		if (id.isEmpty() || !Character.isLetter(id.charAt(0))) id = "lib_" + id;
		return id.length() > 64 ? id.substring(0, 64) : id;
	}

	private static String sha1(byte[] data) {
		try {
			return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-1").digest(data));
		} catch (NoSuchAlgorithmException e) {
			throw new IllegalStateException(e);
		}
	}
}
