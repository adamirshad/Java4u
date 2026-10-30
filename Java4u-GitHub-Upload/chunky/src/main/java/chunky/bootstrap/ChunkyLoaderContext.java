package chunky.bootstrap;

import java.util.Arrays;
import java.util.Set;
import java.util.stream.Collectors;

/** Read-only runtime data exposed by the alpha Fabric Loader compatibility shim. */
public final class ChunkyLoaderContext {
    private static volatile Set<String> modIds = Set.of();

    private ChunkyLoaderContext() { }

    static void initialize(String csv) {
        modIds = Arrays.stream(csv.split(","))
                .map(String::trim)
                .filter(value -> !value.isEmpty())
                .collect(Collectors.toUnmodifiableSet());
    }

    public static boolean isModLoaded(String id) {
        return modIds.contains(id);
    }
}
