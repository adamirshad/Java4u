package net.fabricmc.loader.api;

/** Thrown when a mod's entrypoint cannot be created or fails while running. */
public class EntrypointException extends RuntimeException {
	private final String key;

	public EntrypointException(String key, Throwable cause) {
		super("Exception while loading entries for entrypoint '" + key + "'", cause);
		this.key = key;
	}

	public EntrypointException(String key, String causingMod, Throwable cause) {
		super("Exception while loading entries for entrypoint '" + key + "' provided by '" + causingMod + "'", cause);
		this.key = key;
	}

	public EntrypointException(String message) {
		super(message);
		this.key = "";
	}

	public EntrypointException(Throwable cause) {
		super(cause);
		this.key = "";
	}

	public String getKey() {
		return key;
	}
}
