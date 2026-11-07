package dev.java4u.chunky.loader;

import static dev.java4u.chunky.test.Check.*;

import org.objectweb.asm.ClassReader;
import org.objectweb.asm.ClassWriter;
import org.objectweb.asm.MethodVisitor;
import org.objectweb.asm.Opcodes;
import org.objectweb.asm.tree.AbstractInsnNode;
import org.objectweb.asm.tree.ClassNode;
import org.objectweb.asm.tree.FieldNode;
import org.objectweb.asm.tree.MethodInsnNode;
import org.objectweb.asm.tree.MethodNode;

public final class AccessWidenerTest {
	private AccessWidenerTest() {
	}

	/** A final class test/Sample with a private final field and a private method called from another method. */
	private static byte[] sample() {
		ClassWriter cw = new ClassWriter(ClassWriter.COMPUTE_MAXS);
		cw.visit(Opcodes.V21, Opcodes.ACC_FINAL | Opcodes.ACC_SUPER, "test/Sample", null, "java/lang/Object",
				new String[] { "java/lang/Runnable" });
		cw.visitField(Opcodes.ACC_PRIVATE | Opcodes.ACC_FINAL, "secret", "I", null, null).visitEnd();
		cw.visitField(Opcodes.ACC_PRIVATE, "untouched", "I", null, null).visitEnd();

		MethodVisitor hidden = cw.visitMethod(Opcodes.ACC_PRIVATE, "hidden", "()V", null, null);
		hidden.visitCode();
		hidden.visitInsn(Opcodes.RETURN);
		hidden.visitMaxs(0, 0);
		hidden.visitEnd();

		MethodVisitor run = cw.visitMethod(Opcodes.ACC_PUBLIC, "run", "()V", null, null);
		run.visitCode();
		run.visitVarInsn(Opcodes.ALOAD, 0);
		run.visitMethodInsn(Opcodes.INVOKESPECIAL, "test/Sample", "hidden", "()V", false);
		run.visitInsn(Opcodes.RETURN);
		run.visitMaxs(0, 0);
		run.visitEnd();
		cw.visitEnd();
		return cw.toByteArray();
	}

	private static ClassNode widen(String widener, byte[] bytes) {
		AccessWidener w = new AccessWidener();
		w.read(widener, "test.accesswidener");
		ClassWriter out = new ClassWriter(0);
		new ClassReader(bytes).accept(w.visitor("test.Sample", out), 0);
		ClassNode node = new ClassNode();
		new ClassReader(out.toByteArray()).accept(node, 0);
		return node;
	}

	private static FieldNode field(ClassNode c, String name) {
		for (FieldNode f : c.fields) if (f.name.equals(name)) return f;
		throw new AssertionError("no field " + name);
	}

	private static MethodNode method(ClassNode c, String name) {
		for (MethodNode m : c.methods) if (m.name.equals(name)) return m;
		throw new AssertionError("no method " + name);
	}

	private static boolean has(int access, int flag) {
		return (access & flag) != 0;
	}

	public static void testOpensClassFieldAndMethod() {
		ClassNode c = widen("""
				accessWidener v2 named
				# comments and blank lines are fine

				extendable class test/Sample
				accessible field test/Sample secret I
				mutable field test/Sample secret I
				accessible method test/Sample hidden ()V
				""", sample());
		isTrue(has(c.access, Opcodes.ACC_PUBLIC), "class is public");
		isFalse(has(c.access, Opcodes.ACC_FINAL), "class is no longer final");

		FieldNode secret = field(c, "secret");
		isTrue(has(secret.access, Opcodes.ACC_PUBLIC), "field is public");
		isFalse(has(secret.access, Opcodes.ACC_PRIVATE), "field is not private");
		isFalse(has(secret.access, Opcodes.ACC_FINAL), "mutable field is not final");
		isTrue(has(field(c, "untouched").access, Opcodes.ACC_PRIVATE), "other fields are left alone");

		MethodNode hidden = method(c, "hidden");
		isTrue(has(hidden.access, Opcodes.ACC_PUBLIC), "method is public");
		isTrue(has(hidden.access, Opcodes.ACC_FINAL), "accessible-only method stays non-overridable");
	}

	public static void testPrivateCallsBecomeVirtual() {
		ClassNode c = widen("accessWidener v1 named\naccessible method test/Sample hidden ()V\n", sample());
		boolean found = false;
		for (AbstractInsnNode insn : method(c, "run").instructions) {
			if (insn instanceof MethodInsnNode && ((MethodInsnNode) insn).name.equals("hidden")) {
				eq(Opcodes.INVOKEVIRTUAL, insn.getOpcode(), "call to the opened method");
				found = true;
			}
		}
		isTrue(found, "call to hidden() kept");
	}

	public static void testExtendableMethodIsOverridable() {
		ClassNode c = widen("accessWidener v2 named\nextendable method test/Sample hidden ()V\n", sample());
		MethodNode hidden = method(c, "hidden");
		isTrue(has(hidden.access, Opcodes.ACC_PROTECTED), "private extendable method becomes protected");
		isFalse(has(hidden.access, Opcodes.ACC_FINAL), "and is not final");
	}

	public static void testInjectInterface() {
		ClassNode c = widen("classTweaker v1 named\ninject-interface test/Sample test/Extra<Ljava/lang/String;>\n", sample());
		isTrue(c.interfaces.contains("test/Extra"), "interface added without its generics");
		isTrue(c.interfaces.contains("java/lang/Runnable"), "existing interface kept");
	}

	public static void testTargets() {
		AccessWidener w = new AccessWidener();
		isTrue(w.isEmpty(), "starts empty");
		w.read("accessWidener v2 named\naccessible class net/minecraft/Thing\ntransitive-accessible field a/B c I\n", "t");
		isTrue(w.targets("net.minecraft.Thing"), "class target");
		isTrue(w.targets("a.B"), "transitive entries count");
		isFalse(w.targets("net.minecraft.Other"), "other classes");
	}

	public static void testBadFiles() {
		IllegalArgumentException e = fails(IllegalArgumentException.class,
				() -> new AccessWidener().read("hello\n", "mod.aw"), "missing header");
		contains(e.getMessage(), "mod.aw", "error names the file");
		e = fails(IllegalArgumentException.class,
				() -> new AccessWidener().read("accessWidener v2 named\nmutable class a/B\n", "mod.aw"), "mutable class");
		contains(e.getMessage(), "line 2", "error names the line");
		fails(IllegalArgumentException.class,
				() -> new AccessWidener().read("accessWidener v2 named\naccessible method a/B\n", "t"), "method without descriptor");
		fails(IllegalArgumentException.class,
				() -> new AccessWidener().read("accessWidener v2 named\naccessible thing a/B\n", "t"), "unknown kind");
		// Unknown access types from newer formats are skipped, not fatal.
		AccessWidener w = new AccessWidener();
		w.read("accessWidener v2 named\nfuture-access class a/B\n", "t");
		isTrue(w.isEmpty(), "unknown entry skipped");
	}
}
