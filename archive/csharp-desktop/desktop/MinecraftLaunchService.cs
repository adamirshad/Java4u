using System.Diagnostics;
using System.IO;
using System.Text.Json;
using System.Text.RegularExpressions;

namespace Java4u;

public sealed class MinecraftLaunchService
{
    private const string LauncherName = "Java4u";
    private const string LauncherVersion = "0.5.0";

    public async Task<Process> LaunchAsync(InstanceProfile profile, MinecraftAuthSession account, string microsoftClientId, CancellationToken cancellationToken = default)
    {
        if (!profile.IsInstalled) throw new InvalidOperationException("Install this instance's selected Minecraft version before launching it.");
        var useChunky = string.Equals(profile.Loader, "Chunky", StringComparison.Ordinal);
        if (useChunky && profile.RequiredJavaMajorVersion < 17)
            throw new InvalidOperationException("Chunky alpha requires Java 17 or newer. Choose Vanilla for older Minecraft versions.");
        if (profile.RequiredJavaMajorVersion <= 0) throw new InvalidOperationException("Install the game files again so Java4u can read this version's required Java runtime.");
        if (account.ExpiresUtc <= DateTimeOffset.UtcNow) throw new InvalidOperationException("The Minecraft sign-in has expired. Sign in again before launching.");

        if (!Regex.IsMatch(profile.MinecraftVersion, "^[A-Za-z0-9][A-Za-z0-9._+-]{0,99}$", RegexOptions.CultureInvariant) || profile.MinecraftVersion.Contains("..", StringComparison.Ordinal))
            throw new InvalidDataException("The selected Minecraft version id contains invalid path characters.");
        var instanceRoot = Path.GetFullPath(profile.InstanceDirectory);
        var instancesRoot = Path.GetFullPath(Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Java4u", "instances"));
        if (profile.Id == Guid.Empty || !instanceRoot.StartsWith(instancesRoot + Path.DirectorySeparatorChar, StringComparison.OrdinalIgnoreCase))
            throw new InvalidOperationException("The selected instance folder isn't inside Java4u's data directory.");

        var runtime = await new JavaRuntimeInspector().InspectAsync(profile, cancellationToken);
        if (runtime.DetectedMajorVersion != profile.RequiredJavaMajorVersion)
            throw new InvalidOperationException($"Minecraft {profile.MinecraftVersion} requires Java {profile.RequiredJavaMajorVersion}, but Java {runtime.DetectedMajorVersion} is selected. Choose a matching runtime in instance settings.");

        var gameDirectory = Path.GetFullPath(Path.Combine(instanceRoot, ".minecraft"));
        var versionDirectory = Path.Combine(gameDirectory, "versions", profile.MinecraftVersion);
        var versionMetadataPath = Path.Combine(versionDirectory, profile.MinecraftVersion + ".json");
        var clientPath = Path.Combine(versionDirectory, profile.MinecraftVersion + ".jar");
        if (!File.Exists(versionMetadataPath) || !File.Exists(clientPath)) throw new FileNotFoundException("This instance is missing its installed Minecraft client or version metadata. Install its game files again.");

        using var version = JsonDocument.Parse(await File.ReadAllBytesAsync(versionMetadataPath, cancellationToken));
        var root = version.RootElement;
        if (!root.TryGetProperty("mainClass", out var mainClassElement) || string.IsNullOrWhiteSpace(mainClassElement.GetString()))
            throw new InvalidDataException("This Minecraft version doesn't specify a Java main class.");

        var classpathFiles = GetClasspathFiles(root, gameDirectory, clientPath);
        ChunkyRuntimePlan? chunkyPlan = null;
        if (useChunky)
        {
            var runtimeJar = Path.Combine(AppContext.BaseDirectory, "chunky", "chunky-loader.jar");
            if (!File.Exists(runtimeJar)) throw new FileNotFoundException("The Chunky loader runtime wasn't packaged with Java4u. Repair or reinstall Java4u.", runtimeJar);
            chunkyPlan = await new ChunkyRuntimePlanner().CreatePlanAsync(profile, cancellationToken);
            classpathFiles.Insert(0, runtimeJar);
            var modsDirectory = Path.Combine(gameDirectory, "mods");
            if (Directory.Exists(modsDirectory))
                foreach (var mod in Directory.EnumerateFiles(modsDirectory, "*.jar", SearchOption.TopDirectoryOnly).OrderBy(path => path, StringComparer.OrdinalIgnoreCase))
                    classpathFiles.Add(Path.GetFullPath(mod));
        }
        var missing = classpathFiles.FirstOrDefault(path => !File.Exists(path));
        if (missing is not null) throw new FileNotFoundException("This instance is missing a required library. Use Install game files to repair it.", missing);

        var assetsDirectory = Path.Combine(gameDirectory, "assets");
        var assetIndex = root.TryGetProperty("assetIndex", out var index) && index.TryGetProperty("id", out var indexId) ? indexId.GetString() ?? "" : "";
        if (assetIndex.Length > 0 && (assetIndex.Contains("..", StringComparison.Ordinal) || !Regex.IsMatch(assetIndex, "^[A-Za-z0-9._-]{1,128}$", RegexOptions.CultureInvariant)))
            throw new InvalidDataException("This Minecraft version contains an unsafe asset index id.");
        if (assetIndex.Length > 0 && !File.Exists(Path.Combine(assetsDirectory, "indexes", assetIndex + ".json")))
            throw new FileNotFoundException("This instance is missing its asset index. Use Install game files to repair it.");
        var loggingConfigPath = GetLoggingConfigPath(root, assetsDirectory);
        if (loggingConfigPath.Length > 0 && !File.Exists(loggingConfigPath)) throw new FileNotFoundException("This instance is missing its logging configuration. Use Install game files to repair it.");
        var nativesDirectory = Path.Combine(versionDirectory, "natives");
        if (!Directory.Exists(nativesDirectory)) throw new DirectoryNotFoundException("The instance's native library folder is missing. Use Install game files to repair it.");

        var classpath = string.Join(Path.PathSeparator, classpathFiles.Distinct(StringComparer.OrdinalIgnoreCase));
        var variables = new Dictionary<string, string>(StringComparer.Ordinal)
        {
            ["auth_player_name"] = account.PlayerName,
            ["auth_uuid"] = NormalizeUuid(account.PlayerUuid),
            ["auth_access_token"] = account.MinecraftAccessToken,
            ["auth_xuid"] = account.Xuid,
            ["auth_session"] = $"token:{account.MinecraftAccessToken}:{NormalizeUuid(account.PlayerUuid)}",
            ["user_type"] = "msa",
            ["user_properties"] = "{}",
            ["clientid"] = microsoftClientId,
            ["version_name"] = profile.MinecraftVersion,
            ["version_type"] = root.TryGetProperty("type", out var type) ? type.GetString() ?? "release" : "release",
            ["game_directory"] = gameDirectory,
            ["assets_root"] = assetsDirectory,
            ["assets_index_name"] = assetIndex,
            ["natives_directory"] = nativesDirectory,
            ["library_directory"] = Path.Combine(gameDirectory, "libraries"),
            ["classpath"] = classpath,
            ["classpath_separator"] = Path.PathSeparator.ToString(),
            ["launcher_name"] = LauncherName,
            ["launcher_version"] = LauncherVersion,
            ["resolution_width"] = "854",
            ["resolution_height"] = "480",
            ["path"] = GetLoggingConfigPath(root, assetsDirectory),
        };

        var jvmArguments = ReadStructuredArguments(root, "jvm", variables);
        if (root.TryGetProperty("logging", out var logging) && logging.TryGetProperty("client", out var loggingClient) && loggingClient.TryGetProperty("argument", out var loggingArgument))
            jvmArguments.Add(Expand(loggingArgument.GetString() ?? "", variables));
        if (jvmArguments.Count == 0)
        {
            jvmArguments.Add($"-Djava.library.path={nativesDirectory}");
            jvmArguments.Add("-cp");
            jvmArguments.Add(classpath);
        }
        jvmArguments.RemoveAll(argument => argument.StartsWith("-Xmx", StringComparison.Ordinal));
        jvmArguments.Insert(0, $"-Xmx{profile.MemoryLimitMb}M");
        if (!HasClasspathArgument(jvmArguments))
        {
            jvmArguments.Add("-cp");
            jvmArguments.Add(classpath);
        }
        if (chunkyPlan is not null)
        {
            jvmArguments.Add("-Dchunky.targetMainClass=" + mainClassElement.GetString());
            jvmArguments.Add("-Dchunky.entrypoints=" + chunkyPlan.EncodedEntrypoints);
            jvmArguments.Add("-Dchunky.modIds=" + string.Join(',', chunkyPlan.ModIds));
            jvmArguments.Add("-Dchunky.version=0.1.0-alpha");
        }

        var gameArguments = ReadStructuredArguments(root, "game", variables);
        if (gameArguments.Count == 0 && root.TryGetProperty("minecraftArguments", out var legacyArguments))
            gameArguments = ParseLegacyArguments(legacyArguments.GetString() ?? "", variables);
        if (gameArguments.Count == 0) throw new InvalidDataException("This Minecraft version doesn't contain supported game launch arguments.");

        var startInfo = new ProcessStartInfo
        {
            FileName = runtime.ExecutablePath,
            WorkingDirectory = gameDirectory,
            UseShellExecute = false,
            CreateNoWindow = Path.GetFileName(runtime.ExecutablePath).Equals("javaw.exe", StringComparison.OrdinalIgnoreCase),
        };
        foreach (var argument in jvmArguments) startInfo.ArgumentList.Add(argument);
        startInfo.ArgumentList.Add(chunkyPlan is null ? mainClassElement.GetString()! : "chunky.bootstrap.ChunkyBootstrap");
        foreach (var argument in gameArguments) startInfo.ArgumentList.Add(argument);

        var process = new Process { StartInfo = startInfo, EnableRaisingEvents = true };
        try
        {
            if (!process.Start()) throw new InvalidOperationException("Windows couldn't start the Minecraft Java process.");
            return process;
        }
        catch
        {
            process.Dispose();
            throw;
        }
    }

    private static List<string> GetClasspathFiles(JsonElement version, string gameDirectory, string clientPath)
    {
        var libraryRoot = Path.GetFullPath(Path.Combine(gameDirectory, "libraries"));
        var result = new List<string>();
        if (version.TryGetProperty("libraries", out var libraries) && libraries.ValueKind == JsonValueKind.Array)
        {
            foreach (var library in libraries.EnumerateArray())
            {
                if (!MinecraftCoreInstaller.RulesAllow(library) || !library.TryGetProperty("downloads", out var downloads) || !downloads.TryGetProperty("artifact", out var artifact)) continue;
                var relative = artifact.GetProperty("path").GetString();
                if (string.IsNullOrWhiteSpace(relative) || Path.IsPathRooted(relative) || relative.Contains("..", StringComparison.Ordinal))
                    throw new InvalidDataException("The Minecraft metadata contains an unsafe library path.");
                var path = Path.GetFullPath(Path.Combine(libraryRoot, relative));
                if (!path.StartsWith(libraryRoot + Path.DirectorySeparatorChar, StringComparison.OrdinalIgnoreCase))
                    throw new InvalidDataException("A Minecraft library path escaped its instance folder.");
                result.Add(path);
            }
        }
        result.Add(Path.GetFullPath(clientPath));
        return result.Distinct(StringComparer.OrdinalIgnoreCase).ToList();
    }

    private static List<string> ReadStructuredArguments(JsonElement version, string argumentGroup, IReadOnlyDictionary<string, string> variables)
    {
        var result = new List<string>();
        if (!version.TryGetProperty("arguments", out var arguments) || !arguments.TryGetProperty(argumentGroup, out var entries) || entries.ValueKind != JsonValueKind.Array) return result;
        foreach (var entry in entries.EnumerateArray())
        {
            JsonElement value = entry;
            if (entry.ValueKind == JsonValueKind.Object)
            {
                if (!MinecraftCoreInstaller.RulesAllow(entry) || !entry.TryGetProperty("value", out value)) continue;
            }
            if (value.ValueKind == JsonValueKind.String)
                result.Add(Expand(value.GetString() ?? "", variables));
            else if (value.ValueKind == JsonValueKind.Array)
                foreach (var item in value.EnumerateArray())
                    if (item.ValueKind == JsonValueKind.String) result.Add(Expand(item.GetString() ?? "", variables));
        }
        return result;
    }

    private static List<string> ParseLegacyArguments(string command, IReadOnlyDictionary<string, string> variables)
    {
        var result = new List<string>();
        foreach (Match match in Regex.Matches(command, "\\\"(?<quoted>[^\\\"]*)\\\"|(?<bare>\\S+)", RegexOptions.CultureInvariant))
            result.Add(Expand(match.Groups["quoted"].Success ? match.Groups["quoted"].Value : match.Groups["bare"].Value, variables));
        return result;
    }

    private static string Expand(string argument, IReadOnlyDictionary<string, string> variables)
        => Regex.Replace(argument, "\\$\\{(?<key>[a-zA-Z0-9_]+)\\}", match => variables.TryGetValue(match.Groups["key"].Value, out var value)
            ? value
            : throw new InvalidDataException($"The Minecraft version requires an unsupported launch value: {match.Value}"), RegexOptions.CultureInvariant);

    private static bool HasClasspathArgument(IReadOnlyList<string> arguments)
        => arguments.Any(argument => argument is "-cp" or "-classpath" or "--class-path" || argument.StartsWith("-cp", StringComparison.Ordinal) || argument.StartsWith("--class-path=", StringComparison.Ordinal));

    private static string GetLoggingConfigPath(JsonElement root, string assetsDirectory)
    {
        if (!root.TryGetProperty("logging", out var logging) || !logging.TryGetProperty("client", out var client) || !client.TryGetProperty("file", out var file) || !file.TryGetProperty("id", out var id)) return "";
        var configId = id.GetString();
        if (string.IsNullOrWhiteSpace(configId) || !string.Equals(configId, Path.GetFileName(configId), StringComparison.Ordinal) || configId.Contains("..", StringComparison.Ordinal))
            throw new InvalidDataException("The Minecraft version metadata contains an invalid logging configuration path.");
        return Path.Combine(assetsDirectory, "log_configs", configId);
    }

    private static string NormalizeUuid(string uuid)
    {
        if (!Guid.TryParse(uuid, out var parsed)) throw new InvalidDataException("The signed-in Minecraft profile has an invalid UUID.");
        return parsed.ToString("N");
    }
}
