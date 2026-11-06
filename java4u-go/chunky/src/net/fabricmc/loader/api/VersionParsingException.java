package net.fabricmc.loader.api;

/** Thrown when a version string or version range cannot be parsed. */
public class VersionParsingException extends Exception {
	public VersionParsingException() {
	}

	public VersionParsingException(Throwable cause) {
		super(cause);
	}

	public VersionParsingException(String message) {
		super(message);
	}

	public VersionParsingException(String message, Throwable cause) {
		super(message, cause);
	}
}
