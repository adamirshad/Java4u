package net.fabricmc.loader.api.metadata;

/** An author or contributor listed in mod metadata. */
public interface Person {
	String getName();

	ContactInformation getContact();
}
