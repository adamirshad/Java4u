package net.fabricmc.loader.api;

/** Turns an entrypoint string from a mod's metadata into an object. */
public interface LanguageAdapter {
	static LanguageAdapter getDefault() {
		return dev.java4u.chunky.DefaultLanguageAdapter.INSTANCE;
	}

	<T> T create(ModContainer mod, String value, Class<T> type) throws LanguageAdapterException;
}
