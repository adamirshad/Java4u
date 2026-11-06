package dev.java4u.chunky;

import java.lang.reflect.Constructor;
import java.lang.reflect.Field;
import java.lang.reflect.InvocationHandler;
import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.lang.reflect.Proxy;
import java.util.ArrayList;
import java.util.List;

import net.fabricmc.loader.api.LanguageAdapter;
import net.fabricmc.loader.api.LanguageAdapterException;
import net.fabricmc.loader.api.ModContainer;

/**
 * Creates entrypoints written as "com.example.Mod" (a class), "com.example.Mod::FIELD"
 * (a static field) or "com.example.Mod::method" (a method used as the interface).
 */
public final class DefaultLanguageAdapter implements LanguageAdapter {
	public static final DefaultLanguageAdapter INSTANCE = new DefaultLanguageAdapter();

	private DefaultLanguageAdapter() {
	}

	@Override
	public <T> T create(ModContainer mod, String value, Class<T> type) throws LanguageAdapterException {
		String[] parts = value.split("::", -1);
		if (parts.length > 2 || parts[0].isEmpty()) throw new LanguageAdapterException("bad entrypoint '" + value + "'");
		ClassLoader cl = ChunkyLoader.get().classLoader;
		Class<?> c;
		try {
			c = Class.forName(parts[0], true, cl);
		} catch (ClassNotFoundException e) {
			throw new LanguageAdapterException("class " + parts[0] + " not found", e);
		} catch (LinkageError e) {
			throw new LanguageAdapterException("class " + parts[0] + " failed to load", e);
		}

		if (parts.length == 1) {
			if (!type.isAssignableFrom(c)) {
				throw new LanguageAdapterException(c.getName() + " does not implement " + type.getName());
			}
			return type.cast(newInstance(c));
		}

		String member = parts[1];
		try {
			Field f = c.getDeclaredField(member);
			if (!Modifier.isStatic(f.getModifiers())) throw new LanguageAdapterException(value + " must be a static field");
			f.setAccessible(true);
			Object v = f.get(null);
			if (!type.isInstance(v)) throw new LanguageAdapterException(value + " is not a " + type.getName());
			return type.cast(v);
		} catch (NoSuchFieldException ignored) {
			// Not a field; try a method next.
		} catch (IllegalAccessException e) {
			throw new LanguageAdapterException("can't read " + value, e);
		}

		List<Method> found = new ArrayList<>();
		for (Method m : c.getDeclaredMethods()) if (m.getName().equals(member)) found.add(m);
		if (found.isEmpty()) throw new LanguageAdapterException(value + " has no field or method named " + member);
		if (found.size() > 1) throw new LanguageAdapterException(value + " is ambiguous: several methods are named " + member);
		if (!type.isInterface()) throw new LanguageAdapterException(value + " is a method, so " + type.getName() + " must be an interface");

		Method target = found.get(0);
		target.setAccessible(true);
		Object receiver = Modifier.isStatic(target.getModifiers()) ? null : newInstance(c);
		InvocationHandler handler = (proxy, method, args) -> {
			if (method.getDeclaringClass() == Object.class) {
				switch (method.getName()) {
				case "equals": return proxy == args[0];
				case "hashCode": return System.identityHashCode(proxy);
				default: return "Entrypoint " + value;
				}
			}
			if (method.isDefault()) return InvocationHandler.invokeDefault(proxy, method, args);
			return target.invoke(receiver, args);
		};
		return type.cast(Proxy.newProxyInstance(cl, new Class<?>[] {type}, handler));
	}

	private static Object newInstance(Class<?> c) throws LanguageAdapterException {
		try {
			Constructor<?> ctor = c.getDeclaredConstructor();
			ctor.setAccessible(true);
			return ctor.newInstance();
		} catch (NoSuchMethodException e) {
			throw new LanguageAdapterException(c.getName() + " needs a constructor with no arguments", e);
		} catch (ReflectiveOperationException e) {
			throw new LanguageAdapterException("can't create " + c.getName(), e.getCause() != null ? e.getCause() : e);
		}
	}
}
