package net.fabricmc.loader.api;

import java.util.function.BiConsumer;

/** A shared key/value store mods use to exchange objects without depending on each other. */
public interface ObjectShare {
	Object get(String key);

	void whenAvailable(String key, BiConsumer<String, Object> consumer);

	Object put(String key, Object value);

	Object putIfAbsent(String key, Object value);

	Object remove(String key);
}
