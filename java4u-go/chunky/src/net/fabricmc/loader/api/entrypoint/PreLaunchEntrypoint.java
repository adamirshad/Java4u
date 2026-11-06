package net.fabricmc.loader.api.entrypoint;

/** Runs before the game's main class is loaded ("preLaunch"). */
@FunctionalInterface
public interface PreLaunchEntrypoint {
	void onPreLaunch();
}
