package net.fabricmc.loader.api.entrypoint;

import net.fabricmc.loader.api.ModContainer;

/** An entrypoint object together with the mod that provided it. */
public interface EntrypointContainer<T> {
	T getEntrypoint();

	ModContainer getProvider();

	default String getDefinition() {
		return "";
	}
}
