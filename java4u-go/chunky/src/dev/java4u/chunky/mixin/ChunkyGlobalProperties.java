package dev.java4u.chunky.mixin;

import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;

import org.spongepowered.asm.service.IGlobalPropertyService;
import org.spongepowered.asm.service.IPropertyKey;

/** Mixin's global key/value store, kept in memory for the life of the game. */
public final class ChunkyGlobalProperties implements IGlobalPropertyService {
	private static final class Key implements IPropertyKey {
		private final String name;

		Key(String name) {
			this.name = name;
		}

		@Override
		public boolean equals(Object o) {
			return o instanceof Key && ((Key) o).name.equals(name);
		}

		@Override
		public int hashCode() {
			return name.hashCode();
		}

		@Override
		public String toString() {
			return name;
		}
	}

	private final Map<IPropertyKey, Object> values = new ConcurrentHashMap<>();

	@Override
	public IPropertyKey resolveKey(String name) {
		return new Key(name);
	}

	@Override
	@SuppressWarnings("unchecked")
	public <T> T getProperty(IPropertyKey key) {
		return (T) values.get(key);
	}

	@Override
	public void setProperty(IPropertyKey key, Object value) {
		if (value == null) values.remove(key);
		else values.put(key, value);
	}

	@Override
	@SuppressWarnings("unchecked")
	public <T> T getProperty(IPropertyKey key, T defaultValue) {
		Object v = values.get(key);
		return v == null ? defaultValue : (T) v;
	}

	@Override
	public String getPropertyString(IPropertyKey key, String defaultValue) {
		Object v = values.get(key);
		return v == null ? defaultValue : v.toString();
	}
}
