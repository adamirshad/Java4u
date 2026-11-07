package dev.java4u.chunky.test;

import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.util.Arrays;
import java.util.Comparator;

/**
 * Runs Chunky's tests without a test framework: every public static no-argument method whose
 * name starts with "test" in the classes below. Run with `go run ./tools/buildchunky -test`.
 * Exits with status 1 if any test fails.
 */
public final class TestMain {
	private static final Class<?>[] SUITES = {
			dev.java4u.chunky.util.JsonTest.class,
			dev.java4u.chunky.version.VersionsTest.class,
			dev.java4u.chunky.mod.ModMetaTest.class,
			dev.java4u.chunky.mod.ResolverTest.class,
			dev.java4u.chunky.loader.AccessWidenerTest.class,
	};

	private TestMain() {
	}

	public static void main(String[] args) {
		int passed = 0, failed = 0;
		for (Class<?> suite : SUITES) {
			Method[] methods = suite.getDeclaredMethods();
			Arrays.sort(methods, Comparator.comparing(Method::getName));
			for (Method m : methods) {
				if (!m.getName().startsWith("test") || m.getParameterCount() != 0
						|| !Modifier.isStatic(m.getModifiers()) || !Modifier.isPublic(m.getModifiers())) {
					continue;
				}
				String name = suite.getSimpleName() + "." + m.getName();
				try {
					m.invoke(null);
					passed++;
					System.out.println("ok   " + name);
				} catch (InvocationTargetException e) {
					failed++;
					Throwable cause = e.getCause();
					System.out.println("FAIL " + name + ": " + cause);
					for (StackTraceElement el : cause.getStackTrace()) {
						if (el.getClassName().startsWith("dev.java4u.chunky")) System.out.println("       at " + el);
					}
				} catch (ReflectiveOperationException e) {
					failed++;
					System.out.println("FAIL " + name + ": " + e);
				}
			}
		}
		System.out.println(passed + " passed, " + failed + " failed");
		if (failed > 0 || passed == 0) System.exit(1);
	}
}
