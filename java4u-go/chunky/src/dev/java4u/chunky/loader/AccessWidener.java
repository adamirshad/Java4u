package dev.java4u.chunky.loader;

import java.util.HashMap;
import java.util.HashSet;
import java.util.Map;
import java.util.Set;

import org.objectweb.asm.ClassVisitor;
import org.objectweb.asm.FieldVisitor;
import org.objectweb.asm.MethodVisitor;
import org.objectweb.asm.Opcodes;

/**
 * Applies access widener files ("accessWidener v1|v2 &lt;namespace&gt;"). Each entry opens up
 * a class, method or field: "accessible" makes it public, "extendable" removes final and
 * makes it subclassable or overridable, "mutable" removes final from a field. On
 * unobfuscated versions no renaming is needed, so the namespace is only checked loosely.
 */
public final class AccessWidener {
	private static final int ACCESSIBLE = 1, EXTENDABLE = 2, MUTABLE = 4;

	private static final class Target {
		int classFlags;
		final Map<String, Integer> methods = new HashMap<>();
		final Map<String, Integer> fields = new HashMap<>();
		/** Interfaces to add to the class ("inject-interface" in class tweaker files). */
		final Set<String> injectedInterfaces = new java.util.LinkedHashSet<>();
	}

	/** Keyed by internal name, e.g. net/minecraft/client/Minecraft. */
	private final Map<String, Target> targets = new HashMap<>();
	/** Dotted names, for the fast "does this class need work" check. */
	private final Set<String> dotted = new HashSet<>();

	public boolean isEmpty() {
		return targets.isEmpty();
	}

	public boolean targets(String dottedName) {
		return dotted.contains(dottedName);
	}

	/** Reads one widener file. Problems are reported with the file name and line number. */
	public void read(String text, String source) {
		String[] lines = text.split("\r?\n");
		boolean header = false;
		for (int n = 0; n < lines.length; n++) {
			String line = lines[n];
			int hash = line.indexOf('#');
			if (hash >= 0) line = line.substring(0, hash);
			line = line.trim();
			if (line.isEmpty()) continue;
			String[] t = line.split("\\s+");
			if (!header) {
				boolean widener = t.length >= 3 && t[0].equals("accessWidener") && (t[1].equals("v1") || t[1].equals("v2"));
				boolean tweaker = t.length >= 3 && t[0].equals("classTweaker") && t[1].equals("v1");
				if (!widener && !tweaker) {
					throw new IllegalArgumentException(source + ": not an access widener or class tweaker file");
				}
				header = true;
				continue;
			}
			String access = t[0].startsWith("transitive-") ? t[0].substring("transitive-".length()) : t[0];
			if (access.equals("inject-interface")) {
				// inject-interface <class> <interface>, where the interface may carry generics.
				if (t.length < 3) throw new IllegalArgumentException(source + " line " + (n + 1) + ": inject-interface needs a class and an interface");
				String itf = t[2];
				int generic = itf.indexOf('<');
				if (generic >= 0) itf = itf.substring(0, generic);
				targets.computeIfAbsent(t[1], k -> new Target()).injectedInterfaces.add(itf);
				dotted.add(t[1].replace('/', '.'));
				continue;
			}
			int flag;
			switch (access) {
			case "accessible": flag = ACCESSIBLE; break;
			case "extendable": flag = EXTENDABLE; break;
			case "mutable": flag = MUTABLE; break;
			default: throw new IllegalArgumentException(source + " line " + (n + 1) + ": unknown access '" + t[0] + "'");
			}
			if (t.length < 3) throw new IllegalArgumentException(source + " line " + (n + 1) + ": too few parts");
			String owner = t[2];
			Target target = targets.computeIfAbsent(owner, k -> new Target());
			dotted.add(owner.replace('/', '.'));
			switch (t[1]) {
			case "class":
				if (flag == MUTABLE) throw new IllegalArgumentException(source + " line " + (n + 1) + ": classes can't be mutable");
				target.classFlags |= flag;
				break;
			case "method":
				if (t.length < 5) throw new IllegalArgumentException(source + " line " + (n + 1) + ": method needs a name and descriptor");
				target.methods.merge(t[3] + t[4], flag, (a, b) -> a | b);
				// Opening a member up also has to make its class reachable.
				target.classFlags |= flag == EXTENDABLE ? EXTENDABLE : ACCESSIBLE;
				break;
			case "field":
				if (t.length < 5) throw new IllegalArgumentException(source + " line " + (n + 1) + ": field needs a name and descriptor");
				target.fields.merge(t[3] + ":" + t[4], flag, (a, b) -> a | b);
				if ((flag & ACCESSIBLE) != 0) target.classFlags |= ACCESSIBLE;
				break;
			default:
				throw new IllegalArgumentException(source + " line " + (n + 1) + ": unknown kind '" + t[1] + "'");
			}
		}
	}

	private static int open(int access, int flags, boolean isClass) {
		if ((flags & ACCESSIBLE) != 0) access = (access & ~(Opcodes.ACC_PRIVATE | Opcodes.ACC_PROTECTED)) | Opcodes.ACC_PUBLIC;
		if ((flags & EXTENDABLE) != 0) {
			access &= ~Opcodes.ACC_FINAL;
			if (isClass) {
				access = (access & ~(Opcodes.ACC_PRIVATE | Opcodes.ACC_PROTECTED)) | Opcodes.ACC_PUBLIC;
			} else if ((access & Opcodes.ACC_PRIVATE) != 0) {
				access = (access & ~Opcodes.ACC_PRIVATE) | Opcodes.ACC_PROTECTED;
			}
		}
		return access;
	}

	ClassVisitor visitor(String dottedName, ClassVisitor next) {
		String internal = dottedName.replace('.', '/');
		Target target = targets.get(internal);
		return new ClassVisitor(Opcodes.ASM9, next) {
			/** Private instance methods made non-private must be called virtually inside their class. */
			private final Set<String> nowVirtual = new HashSet<>();

			@Override
			public void visit(int version, int access, String name, String signature, String superName, String[] interfaces) {
				int a = target == null ? access : open(access, target.classFlags, true);
				String[] itfs = interfaces;
				String sig = signature;
				if (target != null && !target.injectedInterfaces.isEmpty()) {
					Set<String> all = new java.util.LinkedHashSet<>(java.util.Arrays.asList(interfaces == null ? new String[0] : interfaces));
					for (String i : target.injectedInterfaces) {
						if (all.add(i) && sig != null) sig = sig + "L" + i + ";"; // keep the generic signature consistent
					}
					itfs = all.toArray(new String[0]);
				}
				super.visit(version, a, name, sig, superName, itfs);
			}

			@Override
			public void visitInnerClass(String name, String outerName, String innerName, int access) {
				Target inner = targets.get(name);
				super.visitInnerClass(name, outerName, innerName, inner == null ? access : open(access, inner.classFlags, true));
			}

			@Override
			public FieldVisitor visitField(int access, String name, String descriptor, String signature, Object value) {
				Integer f = target == null ? null : target.fields.get(name + ":" + descriptor);
				int a = access;
				if (f != null) {
					a = open(a, f & ~MUTABLE, false);
					if ((f & MUTABLE) != 0) a &= ~Opcodes.ACC_FINAL;
				}
				return super.visitField(a, name, descriptor, signature, value);
			}

			@Override
			public MethodVisitor visitMethod(int access, String name, String descriptor, String signature, String[] exceptions) {
				Integer f = target == null ? null : target.methods.get(name + descriptor);
				int a = access;
				if (f != null) {
					boolean wasPrivateInstance = (access & (Opcodes.ACC_PRIVATE | Opcodes.ACC_STATIC)) == Opcodes.ACC_PRIVATE
							&& !name.equals("<init>");
					a = open(a, f, false);
					if (wasPrivateInstance && (a & Opcodes.ACC_PRIVATE) == 0) {
						nowVirtual.add(name + descriptor);
						// Only "accessible" keeps the method from being overridden, matching its old private behavior.
						if ((f & EXTENDABLE) == 0) a |= Opcodes.ACC_FINAL;
					}
				}
				MethodVisitor mv = super.visitMethod(a, name, descriptor, signature, exceptions);
				return new MethodVisitor(Opcodes.ASM9, mv) {
					@Override
					public void visitMethodInsn(int opcode, String owner, String mname, String mdesc, boolean itf) {
						if (opcode == Opcodes.INVOKESPECIAL && owner.equals(internal) && !mname.equals("<init>")
								&& isNowVirtual(mname + mdesc)) {
							opcode = itf ? Opcodes.INVOKEINTERFACE : Opcodes.INVOKEVIRTUAL;
						}
						super.visitMethodInsn(opcode, owner, mname, mdesc, itf);
					}
				};
			}

			private boolean isNowVirtual(String key) {
				if (nowVirtual.contains(key)) return true;
				// The call may appear before the method itself in the class file.
				Integer f = target == null ? null : target.methods.get(key);
				return f != null;
			}
		};
	}
}
