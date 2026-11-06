package net.fabricmc.loader.api;

import java.util.Collection;

/** Translates names between mapping namespaces. On unobfuscated versions every name maps to itself. */
public interface MappingResolver {
	Collection<String> getNamespaces();

	String getCurrentRuntimeNamespace();

	String mapClassName(String namespace, String className);

	String unmapClassName(String targetNamespace, String className);

	String mapFieldName(String namespace, String owner, String name, String descriptor);

	String mapMethodName(String namespace, String owner, String name, String descriptor);
}
