package dev.java4u.chunky;

import java.util.concurrent.atomic.AtomicBoolean;

import net.fabricmc.api.ClientModInitializer;
import net.fabricmc.api.DedicatedServerModInitializer;
import net.fabricmc.api.ModInitializer;

/** Called from the patched game code. Runs mods' initializers exactly once. */
public final class ChunkyHooks {
	private static final AtomicBoolean STARTED = new AtomicBoolean();

	private ChunkyHooks() {
	}

	/** Inserted into Minecraft's constructor by Chunky. */
	public static void startClient(Object minecraft) {
		if (!STARTED.compareAndSet(false, true)) return;
		ChunkyLoader loader = ChunkyLoader.get();
		loader.setGameInstance(minecraft);
		long start = System.nanoTime();
		loader.invokeEntrypoints("main", ModInitializer.class, ModInitializer::onInitialize);
		loader.invokeEntrypoints("client", ClientModInitializer.class, ClientModInitializer::onInitializeClient);
		Log.info("Mods initialized in " + (System.nanoTime() - start) / 1_000_000 + " ms");
	}

	/** Inserted into the dedicated server's main method, after the registries exist. */
	public static void startServer(Object server) {
		if (!STARTED.compareAndSet(false, true)) return;
		ChunkyLoader loader = ChunkyLoader.get();
		if (server != null) loader.setGameInstance(server);
		long start = System.nanoTime();
		loader.invokeEntrypoints("main", ModInitializer.class, ModInitializer::onInitialize);
		loader.invokeEntrypoints("server", DedicatedServerModInitializer.class, DedicatedServerModInitializer::onInitializeServer);
		Log.info("Mods initialized in " + (System.nanoTime() - start) / 1_000_000 + " ms");
	}
}
