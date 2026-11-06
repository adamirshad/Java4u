package net.fabricmc.api;

/** Common entrypoint ("main"): runs on both client and server. Provided by Chunky. */
@FunctionalInterface
public interface ModInitializer {
	void onInitialize();
}
