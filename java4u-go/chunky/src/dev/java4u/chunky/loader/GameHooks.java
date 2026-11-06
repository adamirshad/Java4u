package dev.java4u.chunky.loader;

import org.objectweb.asm.ClassReader;
import org.objectweb.asm.ClassWriter;
import org.objectweb.asm.Opcodes;
import org.objectweb.asm.tree.AbstractInsnNode;
import org.objectweb.asm.tree.ClassNode;
import org.objectweb.asm.tree.FieldInsnNode;
import org.objectweb.asm.tree.InsnList;
import org.objectweb.asm.tree.MethodInsnNode;
import org.objectweb.asm.tree.MethodNode;
import org.objectweb.asm.tree.VarInsnNode;

import dev.java4u.chunky.Log;

/**
 * Inserts the call that starts mods into the game's own code. Because 26.x and newer ship
 * with real class names, Chunky can find the right spot by name instead of guessing.
 *
 * Client: in {@code net.minecraft.client.Minecraft}'s constructor, right after the game
 * stores itself in its static instance field (and its game directory, when that comes
 * next). At that point Minecraft.getInstance() works but no resources are loaded yet.
 */
final class GameHooks {
	static final String CLIENT = "net.minecraft.client.Minecraft";
	private static final String CLIENT_INTERNAL = "net/minecraft/client/Minecraft";
	private static final String HOOKS = "dev/java4u/chunky/ChunkyHooks";
	/** Mixin reads the class too, so patch() runs twice; log once. */
	private static volatile boolean logged;

	private GameHooks() {
	}

	static boolean targets(String name) {
		return name.equals(CLIENT);
	}

	static byte[] patch(String name, byte[] bytes) {
		ClassNode node = new ClassNode();
		new ClassReader(bytes).accept(node, 0);
		boolean done = false;
		for (MethodNode m : node.methods) {
			if (!m.name.equals("<init>")) continue;
			AbstractInsnNode at = findClientHookPoint(m, node.superName);
			if (at == null) continue;
			InsnList call = new InsnList();
			call.add(new VarInsnNode(Opcodes.ALOAD, 0));
			call.add(new MethodInsnNode(Opcodes.INVOKESTATIC, HOOKS, "startClient", "(Ljava/lang/Object;)V", false));
			m.instructions.insert(at, call);
			m.maxStack = Math.max(m.maxStack, 1);
			done = true;
			break;
		}
		if (!done) {
			Log.warn("Couldn't find where to start mods in " + name + "; mods' initializers won't run. "
					+ "This version of Minecraft may not be supported by Chunky yet.");
			return bytes;
		}
		if (!logged) {
			logged = true;
			Log.info("Hooked mod startup into " + name);
		}
		ClassWriter w = new ClassWriter(0);
		node.accept(w);
		return w.toByteArray();
	}

	/**
	 * After the game has stored itself ("instance = this") and its player ("this.user = ..."),
	 * so mods can call Minecraft.getInstance().getUser() while they start, as they can on
	 * Fabric. Falls back to the game directory field, then to just after "instance = this".
	 */
	private static AbstractInsnNode findClientHookPoint(MethodNode ctor, String superName) {
		AbstractInsnNode superCall = null, instance = null, user = null, dir = null;
		for (AbstractInsnNode n = ctor.instructions.getFirst(); n != null; n = n.getNext()) {
			int op = n.getOpcode();
			if (superCall == null && op == Opcodes.INVOKESPECIAL) {
				MethodInsnNode mi = (MethodInsnNode) n;
				if (mi.name.equals("<init>") && mi.owner.equals(superName)) superCall = n;
			} else if (op == Opcodes.PUTSTATIC && instance == null) {
				FieldInsnNode f = (FieldInsnNode) n;
				if (f.owner.equals(CLIENT_INTERNAL) && f.desc.equals("L" + CLIENT_INTERNAL + ";")) instance = n;
			} else if (op == Opcodes.PUTFIELD && instance != null) {
				FieldInsnNode f = (FieldInsnNode) n;
				if (!f.owner.equals(CLIENT_INTERNAL)) continue;
				if (user == null && f.desc.equals("Lnet/minecraft/client/User;")) user = n;
				if (dir == null && (f.desc.equals("Ljava/io/File;") || f.desc.equals("Ljava/nio/file/Path;"))) dir = n;
			}
		}
		if (user != null) return dir != null && isAfter(dir, user) ? dir : user;
		if (dir != null) return dir;
		return instance != null ? instance : superCall;
	}

	private static boolean isAfter(AbstractInsnNode a, AbstractInsnNode b) {
		for (AbstractInsnNode n = b.getNext(); n != null; n = n.getNext()) if (n == a) return true;
		return false;
	}
}
