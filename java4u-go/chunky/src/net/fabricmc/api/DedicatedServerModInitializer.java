package net.fabricmc.api;

/** Dedicated server entrypoint ("server"). Provided by Chunky. */
@FunctionalInterface
public interface DedicatedServerModInitializer {
	void onInitializeServer();
}
