package dev.java4u.chunky.test;

import java.util.Objects;

/** Small assertions for Chunky's tests. */
public final class Check {
	private Check() {
	}

	/** Code that may throw, for {@link #fails}. */
	public interface Body {
		void run() throws Exception;
	}

	public static void isTrue(boolean condition, String what) {
		if (!condition) throw new AssertionError(what);
	}

	public static void isFalse(boolean condition, String what) {
		if (condition) throw new AssertionError(what);
	}

	public static void eq(Object want, Object got, String what) {
		if (!Objects.equals(want, got)) throw new AssertionError(what + ": want <" + want + ">, got <" + got + ">");
	}

	public static void contains(String text, String part, String what) {
		if (text == null || !text.contains(part)) throw new AssertionError(what + ": <" + text + "> doesn't contain <" + part + ">");
	}

	/** Checks that body throws an exception of the given type, and returns it. */
	public static <T extends Throwable> T fails(Class<T> type, Body body, String what) {
		try {
			body.run();
		} catch (Throwable t) {
			if (type.isInstance(t)) return type.cast(t);
			throw new AssertionError(what + ": threw " + t + " instead of " + type.getSimpleName(), t);
		}
		throw new AssertionError(what + ": didn't throw " + type.getSimpleName());
	}
}
