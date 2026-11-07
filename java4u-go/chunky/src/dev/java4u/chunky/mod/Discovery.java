package dev.java4u.chunky.mod;

import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.DirectoryStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.StandardCopyOption;
import java.util.ArrayList;
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

	/**
	 * Every mod jar (and nested jar) under modsDir, in file-name order. Built-in mods are
	 * added by the caller. Top-level jars are read in parallel; each only needs its zip
	 * index and its mod json, so this is mostly waiting on the disk.
	 */
	public static List<ChunkyMod> scan(Path modsDir, Path cacheDir) throws IOException {
		Discovery d = new Discovery(cacheDir);
		if (!Files.isDirectory(modsDir)) return d.found;
		List<Path> jars = new ArrayList<>();
		try (DirectoryStream<Path> ds = Files.newDirectoryStream(modsDir, "*.jar")) {
			for (Path p : ds) if (Files.isRegularFile(p)) jars.add(p);
		}
		jars.sort(null);

		List<List<ChunkyMod>> perJar = new ArrayList<>();
		List<java.util.concurrent.Future<List<ChunkyMod>>> futures = new ArrayList<>();
		int threads = Math.max(1, Math.min(8, Runtime.getRuntime().availableProcessors()));
		java.util.concurrent.ExecutorService pool = java.util.concurrent.Executors.newFixedThreadPool(threads, r -> {
			Thread t = new Thread(r, "Chunky discovery");
			t.setDaemon(true);
			return t;
		});
		try {
			for (Path jar : jars) {
				futures.add(pool.submit(() -> {
					List<ChunkyMod> out = new ArrayList<>();
					try {
						if (d.read(jar, null, null, 0, out) == null) {
							Log.warn(jar.getFileName() + " is not a Chunky or Fabric mod; skipping it.");
						}
					} catch (IOException | RuntimeException e) {
						throw new IOException("Can't read mod " + jar.getFileName() + ": " + e.getMessage(), e);
					}
					return out;
				}));
			}
			for (java.util.concurrent.Future<List<ChunkyMod>> f : futures) {
				try {
					perJar.add(f.get());
				} catch (java.util.concurrent.ExecutionException e) {
					if (e.getCause() instanceof IOException io) throw io;
					throw new IOException(e.getCause().getMessage(), e.getCause());
				} catch (InterruptedException e) {
					Thread.currentThread().interrupt();
					throw new IOException("interrupted while reading mods", e);
				}
			}
		} finally {
			pool.shutdownNow();
		}
		for (List<ChunkyMod> l : perJar) d.found.addAll(l);
		d.pruneCache();
		return d.found;
	}

	/** Deletes cached nested jars no mod uses any more (old versions, removed mods). */
	private void pruneCache() {
		java.util.Set<Path> used = new java.util.HashSet<>();
		for (ChunkyMod m : found) if (m.jar != null) used.add(m.jar.toAbsolutePath().normalize());
		if (!Files.isDirectory(cacheDir)) return;
		try (DirectoryStream<Path> ds = Files.newDirectoryStream(cacheDir, "*.jar")) {
			for (Path p : ds) {
				if (!used.contains(p.toAbsolutePath().normalize())) Files.deleteIfExists(p);
			}
		} catch (IOException e) {
			Log.warn("Could not tidy the nested-jar cache: " + e.getMessage());
		}
	}

	private ChunkyMod read(Path jar, ChunkyMod parent, String subLocation, int depth, List<ChunkyMod> out) throws IOException {
		ModMeta meta;
		List<Path> nestedPaths = new ArrayList<>();
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
				if (depth < MAX_DEPTH) {
					for (String nested : meta.nestedJars) {
						ZipEntry e = zip.getEntry(nested);
						if (e == null) {
							Log.warn(meta.getId() + " lists missing nested jar " + nested);
							continue;
						}
						nestedPaths.add(cache(zip, e));
						nestedNames.add(nested);
					}
				} else if (!meta.nestedJars.isEmpty()) {
					Log.warn(meta.getId() + ": nested jars are too deep; ignoring the rest");
				}
			}
		}
		ChunkyMod mod = new ChunkyMod(meta, jar, parent, subLocation);
		out.add(mod);
		for (int i = 0; i < nestedPaths.size(); i++) {
			read(nestedPaths.get(i), mod, nestedNames.get(i), depth + 1, out);
		}
		return mod;
	}

	/**
	 * Copies a nested jar into the cache unless it's already there. The file is named by the
	 * CRC-32 and size from the zip's index, so finding a cached copy needs no unpacking and
	 * a changed jar always gets a new name.
	 */
	private Path cache(ZipFile zip, ZipEntry e) throws IOException {
		String name = e.getName();
		String base = stripJar(name.substring(name.lastIndexOf('/') + 1)).replaceAll("[^A-Za-z0-9._+-]", "_");
		long crc = e.getCrc(), size = e.getSize();
		Path out = cacheDir.resolve(String.format("%08x-%d-%s.jar", crc, size, base));
		if (crc >= 0 && size >= 0 && Files.isRegularFile(out) && Files.size(out) == size) return out;

		Files.createDirectories(cacheDir);
		Path tmp = Files.createTempFile(cacheDir, "nested", ".tmp");
		try {
			java.util.zip.CRC32 check = new java.util.zip.CRC32();
			try (InputStream in = new java.util.zip.CheckedInputStream(zip.getInputStream(e), check)) {
				Files.copy(in, tmp, StandardCopyOption.REPLACE_EXISTING);
			}
			if (crc >= 0 && check.getValue() != crc) throw new IOException(name + " is damaged (CRC mismatch)");
			Files.move(tmp, out, StandardCopyOption.REPLACE_EXISTING, StandardCopyOption.ATOMIC_MOVE);
		} finally {
			Files.deleteIfExists(tmp);
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


}
