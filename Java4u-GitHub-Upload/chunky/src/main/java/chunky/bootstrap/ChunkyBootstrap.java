package chunky.bootstrap;

import net.fabricmc.api.ClientModInitializer;
import net.fabricmc.api.ModInitializer;

import java.lang.reflect.Constructor;
import java.lang.reflect.InvocationTargetException;
import java.io.IOException;
import java.io.PrintWriter;
import java.io.StringWriter;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.charset.StandardCharsets;
import java.util.Base64;
import java.util.List;

/** Experimental entrypoint bridge for the deliberately small Chunky alpha surface. */
public final class ChunkyBootstrap {
    private ChunkyBootstrap() { }

    public static void main(String[] arguments) throws Exception {
        String targetMain = requiredProperty("chunky.targetMainClass");
        String encodedEntrypoints = System.getProperty("chunky.entrypoints", "");
        String modIds = System.getProperty("chunky.modIds", "");
        ChunkyLoaderContext.initialize(modIds);

        try {
            log("Chunky " + System.getProperty("chunky.version", "unknown") + " starting; mods=" + modIds);
            for (String item : encodedEntrypoints.split(",")) {
                if (item.isBlank()) continue;
                String entrypoint = new String(Base64.getUrlDecoder().decode(item), StandardCharsets.UTF_8);
                String[] parts = entrypoint.split("\\t", 3);
                if (parts.length != 3) throw new IllegalStateException("Invalid Chunky entrypoint plan.");
                log("Initializing " + parts[0] + " " + parts[1] + " entrypoint " + parts[2]);
                invoke(parts[0], parts[1], parts[2]);
            }

            log("Starting Minecraft main class " + targetMain);
            Class<?> gameMain = Class.forName(targetMain, true, ClassLoader.getSystemClassLoader());
            gameMain.getMethod("main", String[].class).invoke(null, (Object) arguments);
        } catch (InvocationTargetException exception) {
            Throwable cause = exception.getCause();
            logFailure(cause);
            if (cause instanceof Exception checked) throw checked;
            if (cause instanceof Error error) throw error;
            throw exception;
        } catch (Exception | Error failure) {
            logFailure(failure);
            throw failure;
        }
    }

    private static void invoke(String modId, String kind, String className) throws Exception {
        Class<?> type = Class.forName(className, true, ClassLoader.getSystemClassLoader());
        Constructor<?> constructor = type.getDeclaredConstructor();
        if (!constructor.canAccess(null)) constructor.setAccessible(true);
        Object initializer = constructor.newInstance();
        if (kind.equals("main") && initializer instanceof ModInitializer common) {
            common.onInitialize();
        } else if (kind.equals("client") && initializer instanceof ClientModInitializer client) {
            client.onInitializeClient();
        } else {
            throw new IllegalStateException("Mod '" + modId + "' entrypoint '" + className +
                    "' doesn't implement the supported Fabric initializer interface.");
        }
    }

    private static String requiredProperty(String name) {
        String value = System.getProperty(name);
        if (value == null || value.isBlank()) throw new IllegalStateException("Missing required Chunky launch setting: " + name);
        return value;
    }

    private static void logFailure(Throwable failure) {
        StringWriter text = new StringWriter();
        failure.printStackTrace(new PrintWriter(text));
        log(text.toString());
    }

    private static void log(String message) {
        try {
            Path logFile = Path.of(System.getProperty("user.dir"), "logs", "chunky-latest.log");
            Files.createDirectories(logFile.getParent());
            Files.writeString(logFile, java.time.Instant.now() + " " + message + System.lineSeparator(),
                    StandardCharsets.UTF_8, java.nio.file.StandardOpenOption.CREATE, java.nio.file.StandardOpenOption.APPEND);
        } catch (IOException ignored) { }
    }
}
