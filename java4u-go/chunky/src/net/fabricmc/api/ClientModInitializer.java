package net.fabricmc.api;

/** Client entrypoint ("client"). Provided by Chunky. */
@FunctionalInterface
public interface ClientModInitializer {
	void onInitializeClient();
}
