package net.fabricmc.loader.api;

/** Thrown by a {@link LanguageAdapter} that cannot create an entrypoint object. */
public class LanguageAdapterException extends Exception {
	public LanguageAdapterException(String message) {
		super(message);
	}

	public LanguageAdapterException(Throwable cause) {
		super(cause);
	}

	public LanguageAdapterException(String message, Throwable cause) {
		super(message, cause);
	}
}
