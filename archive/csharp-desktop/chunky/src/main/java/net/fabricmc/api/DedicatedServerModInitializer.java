package net.fabricmc.api;

/** Fabric-compatible server initializer contract. Chunky alpha is client-only. */
public interface DedicatedServerModInitializer {
    void onInitializeServer();
}
