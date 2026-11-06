package dev.java4u.chunky.loader;

import org.objectweb.asm.ClassReader;
import org.objectweb.asm.ClassWriter;
import org.spongepowered.asm.mixin.MixinEnvironment;
import org.spongepowered.asm.mixin.transformer.IMixinTransformer;

/**
 * Turns class bytes from a jar into the bytes that get defined. Order: access wideners,
 * Chunky's game hooks, then Mixin. Classes nothing touches are returned unchanged
 * without ever being parsed, which keeps startup fast.
 */
public class Transformer {
	public static final Transformer NONE = new Transformer(null, null);

	private final AccessWidener widener;
	private final IMixinTransformer mixin;

	public Transformer(AccessWidener widener, IMixinTransformer mixin) {
		this.widener = widener;
		this.mixin = mixin;
	}

	/** Chunky's own patches only: what Mixin reads as the "original" class. */
	public byte[] preMixin(String name, byte[] bytes) {
		boolean widen = widener != null && widener.targets(name);
		boolean hook = GameHooks.targets(name);
		if (!widen && !hook) return bytes;

		ClassReader reader = new ClassReader(bytes);
		ClassWriter writer = new ClassWriter(reader, 0);
		if (hook) {
			bytes = GameHooks.patch(name, bytes);
			reader = new ClassReader(bytes);
			writer = new ClassWriter(reader, 0);
		}
		if (!widen) return bytes;
		reader.accept(widener.visitor(name, writer), 0);
		return writer.toByteArray();
	}

	public byte[] transform(String name, byte[] bytes) {
		byte[] b = preMixin(name, bytes);
		if (mixin == null) return b;
		return mixin.transformClassBytes(name, name, b);
	}

	/** Bytes for a class Mixin creates on the fly, or null. */
	public byte[] generate(String name) {
		if (mixin == null) return null;
		return mixin.generateClass(MixinEnvironment.getCurrentEnvironment(), name);
	}
}
