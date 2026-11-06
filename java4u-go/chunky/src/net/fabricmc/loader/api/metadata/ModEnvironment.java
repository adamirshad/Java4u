package net.fabricmc.loader.api.metadata;

import net.fabricmc.api.EnvType;

/** Which side a mod runs on. */
public enum ModEnvironment {
	CLIENT,
	SERVER,
	UNIVERSAL;

	public boolean matches(EnvType type) {
		switch (this) {
		case CLIENT:
			return type == EnvType.CLIENT;
		case SERVER:
			return type == EnvType.SERVER;
		default:
			return true;
		}
	}
}
