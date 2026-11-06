package dev.java4u.chunky.mixin;

import org.spongepowered.asm.logging.Level;
import org.spongepowered.asm.logging.LoggerAdapterAbstract;

import dev.java4u.chunky.Log;

/**
 * Sends Mixin's messages to Chunky's log. Warnings and errors always show; set
 * -Dchunky.debug=true to see Mixin's info and debug messages too.
 */
final class ChunkyMixinLogger extends LoggerAdapterAbstract {
	private static final boolean DEBUG = Boolean.getBoolean("chunky.debug");

	ChunkyMixinLogger(String id) {
		super(id);
	}

	@Override
	public String getType() {
		return "Chunky";
	}

	@Override
	public void catching(Level level, Throwable t) {
		log(level, "Caught exception", t);
	}

	@Override
	public void log(Level level, String message, Object... params) {
		Throwable t = params.length > 0 && params[params.length - 1] instanceof Throwable ? (Throwable) params[params.length - 1] : null;
		write(level, format(message, params), t);
	}

	@Override
	public void log(Level level, String message, Throwable t) {
		write(level, message, t);
	}

	@Override
	public <T extends Throwable> T throwing(T t) {
		write(Level.WARN, "Throwing", t);
		return t;
	}

	private void write(Level level, String message, Throwable t) {
		String text = "[Mixin] " + message;
		switch (level) {
		case FATAL:
		case ERROR:
			Log.error(text, t);
			break;
		case WARN:
			Log.warn(t == null ? text : text + ": " + t);
			break;
		default:
			if (DEBUG) Log.info(text);
		}
	}

	/** Fills "{}" placeholders the way Mixin's messages expect. */
	private static String format(String message, Object... params) {
		if (message == null || params.length == 0) return String.valueOf(message);
		StringBuilder sb = new StringBuilder();
		int p = 0, i = 0;
		while (i < message.length()) {
			int at = message.indexOf("{}", i);
			if (at < 0 || p >= params.length) {
				sb.append(message, i, message.length());
				break;
			}
			sb.append(message, i, at).append(params[p++]);
			i = at + 2;
		}
		return sb.toString();
	}
}
