package dev.java4u.chunky.mixin;

import java.io.IOException;
import java.io.InputStream;
import java.net.URL;
import java.util.Collection;
import java.util.Collections;
import java.util.List;

import org.objectweb.asm.ClassReader;
import org.objectweb.asm.tree.ClassNode;
import org.spongepowered.asm.launch.platform.container.ContainerHandleVirtual;
import org.spongepowered.asm.launch.platform.container.IContainerHandle;
import org.spongepowered.asm.mixin.MixinEnvironment;
import org.spongepowered.asm.mixin.transformer.IMixinTransformer;
import org.spongepowered.asm.mixin.transformer.IMixinTransformerFactory;
import org.spongepowered.asm.service.IAdviceProvider;
import org.spongepowered.asm.service.IClassBytecodeProvider;
import org.spongepowered.asm.service.IClassProvider;
import org.spongepowered.asm.service.IClassTracker;
import org.spongepowered.asm.service.IMixinAuditTrail;
import org.spongepowered.asm.service.IMixinInternal;
import org.spongepowered.asm.service.ITransformer;
import org.spongepowered.asm.service.ITransformerProvider;
import org.spongepowered.asm.service.MixinServiceAbstract;

import dev.java4u.chunky.loader.ChunkyClassLoader;

/** Tells Mixin how to find and read classes through Chunky's class loader. */
public final class ChunkyMixinService extends MixinServiceAbstract
		implements IClassProvider, IClassBytecodeProvider, ITransformerProvider, IClassTracker {
	private static volatile ChunkyClassLoader loader;
	private static volatile IMixinTransformer transformer;

	public static void setLoader(ChunkyClassLoader l) {
		loader = l;
	}

	public static IMixinTransformer transformer() {
		return transformer;
	}

	@Override
	public String getName() {
		return "Chunky";
	}

	@Override
	public boolean isValid() {
		return loader != null;
	}

	@Override
	public void offer(IMixinInternal internal) {
		if (internal instanceof IMixinTransformerFactory) {
			transformer = ((IMixinTransformerFactory) internal).createTransformer();
		}
		super.offer(internal);
	}

	@Override
	public IClassProvider getClassProvider() {
		return this;
	}

	@Override
	public IClassBytecodeProvider getBytecodeProvider() {
		return this;
	}

	@Override
	public ITransformerProvider getTransformerProvider() {
		return this;
	}

	@Override
	public IClassTracker getClassTracker() {
		return this;
	}

	@Override
	public IMixinAuditTrail getAuditTrail() {
		return null;
	}

	@Override
	public IAdviceProvider getAdviceProvider() {
		return null;
	}

	@Override
	public org.spongepowered.asm.service.IFeatureValidator getFeatureValidator() {
		return null;
	}

	@Override
	public Collection<String> getPlatformAgents() {
		return Collections.singletonList("org.spongepowered.asm.launch.platform.MixinPlatformAgentDefault");
	}

	@Override
	public IContainerHandle getPrimaryContainer() {
		return new ContainerHandleVirtual(getName());
	}

	@Override
	public InputStream getResourceAsStream(String name) {
		return loader.getResourceAsStream(name);
	}

	@Override
	public MixinEnvironment.CompatibilityLevel getMinCompatibilityLevel() {
		return MixinEnvironment.CompatibilityLevel.JAVA_8;
	}

	@Override
	public MixinEnvironment.CompatibilityLevel getMaxCompatibilityLevel() {
		return MixinEnvironment.CompatibilityLevel.MAX_SUPPORTED;
	}

	// ---------------------------------------------------------------- IClassProvider

	@Override
	@Deprecated
	public URL[] getClassPath() {
		return new URL[0];
	}

	@Override
	public Class<?> findClass(String name) throws ClassNotFoundException {
		return Class.forName(name, true, loader);
	}

	@Override
	public Class<?> findClass(String name, boolean initialize) throws ClassNotFoundException {
		return Class.forName(name, initialize, loader);
	}

	@Override
	public Class<?> findAgentClass(String name, boolean initialize) throws ClassNotFoundException {
		return Class.forName(name, initialize, ChunkyMixinService.class.getClassLoader());
	}

	// ---------------------------------------------------------------- IClassBytecodeProvider

	@Override
	public ClassNode getClassNode(String name) throws ClassNotFoundException, IOException {
		return getClassNode(name, true);
	}

	@Override
	public ClassNode getClassNode(String name, boolean runTransformers) throws ClassNotFoundException, IOException {
		return getClassNode(name, runTransformers, 0);
	}

	@Override
	public ClassNode getClassNode(String name, boolean runTransformers, int readerFlags)
			throws ClassNotFoundException, IOException {
		String dotted = name.replace('/', '.');
		byte[] bytes = loader.getPreMixinBytes(dotted);
		if (bytes == null) throw new ClassNotFoundException(dotted);
		ClassNode node = new ClassNode();
		new ClassReader(bytes).accept(node, readerFlags);
		return node;
	}

	// ---------------------------------------------------------------- ITransformerProvider

	@Override
	public Collection<ITransformer> getTransformers() {
		return Collections.emptyList();
	}

	@Override
	public Collection<ITransformer> getDelegatedTransformers() {
		return Collections.emptyList();
	}

	@Override
	public void addTransformerExclusion(String name) {
	}

	// ---------------------------------------------------------------- IClassTracker

	@Override
	public void registerInvalidClass(String name) {
		loader.markInvalid(name.replace('/', '.'));
	}

	@Override
	public boolean isClassLoaded(String name) {
		return loader.isLoaded(name.replace('/', '.'));
	}

	@Override
	public String getClassRestrictions(String name) {
		return "";
	}

	/** Mixin services are looked up by class name; nothing else is needed at bootstrap. */
	public static final class Bootstrap implements org.spongepowered.asm.service.IMixinServiceBootstrap {
		@Override
		public String getName() {
			return "Chunky";
		}

		@Override
		public String getServiceClassName() {
			return ChunkyMixinService.class.getName();
		}

		@Override
		public void bootstrap() {
		}
	}

	static List<String> none() {
		return Collections.emptyList();
	}
}
