package dev.java4u.chunky;

import java.io.IOException;
import java.io.PrintWriter;
import java.io.StringWriter;
import java.io.Writer;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.LocalTime;
import java.time.format.DateTimeFormatter;

/** Chunky's log: printed to the console (Java4U shows it) and kept in logs/chunky-latest.log. */
public final class Log {
	private static final DateTimeFormatter TIME = DateTimeFormatter.ofPattern("HH:mm:ss");
	private static Writer file;

	private Log() {
	}

	static synchronized void init(Path gameDir) {
		try {
			Path logs = gameDir.resolve("logs");
			Files.createDirectories(logs);
			file = Files.newBufferedWriter(logs.resolve("chunky-latest.log"), StandardCharsets.UTF_8);
		} catch (IOException e) {
			System.err.println("[Chunky] can't open log file: " + e);
		}
	}

	public static void info(String msg) {
		write("INFO", msg);
	}

	public static void warn(String msg) {
		write("WARN", msg);
	}

	public static void error(String msg, Throwable t) {
		StringWriter sw = new StringWriter();
		if (t != null) t.printStackTrace(new PrintWriter(sw));
		write("ERROR", t == null ? msg : msg + System.lineSeparator() + sw);
	}

	private static synchronized void write(String level, String msg) {
		String line = "[" + LocalTime.now().format(TIME) + "] [Chunky/" + level + "] " + msg;
		(level.equals("INFO") ? System.out : System.err).println(line);
		if (file != null) {
			try {
				file.write(line);
				file.write(System.lineSeparator());
				file.flush();
			} catch (IOException ignored) {
				// Logging must never break the game.
			}
		}
	}
}
