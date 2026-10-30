using System.IO;
using System.IO.Compression;
using System.Text.Json;
using System.Text.RegularExpressions;

namespace Java4u;

public sealed record ChunkyEntrypoint(string ModId, string Kind, string ClassName);
public sealed record ChunkyModRecord(string FileName, string Id, string Version, string Environment, IReadOnlyList<string> RequiredMods, IReadOnlyList<ChunkyEntrypoint> Entrypoints);
public sealed record ChunkyModIssue(string FileName, string Message);
public sealed record ChunkyModReport(IReadOnlyList<ChunkyModRecord> Mods, IReadOnlyList<ChunkyModIssue> Issues);

/// <summary>Read-only first step for Chunky's Fabric metadata compatibility layer.</summary>
public sealed class ChunkyModInspector
{
    private const long MaximumMetadataBytes = 4 * 1024 * 1024;
    private static readonly Regex ModIdPattern = new("^[a-z0-9][a-z0-9_-]*$", RegexOptions.CultureInvariant | RegexOptions.Compiled);

    public Task<ChunkyModReport> InspectAsync(InstanceProfile profile, CancellationToken cancellationToken = default)
        => Task.Run(() => Inspect(profile, cancellationToken), cancellationToken);

    private static ChunkyModReport Inspect(InstanceProfile profile, CancellationToken cancellationToken)
    {
        var modDirectory = Path.Combine(profile.InstanceDirectory, ".minecraft", "mods");
        Directory.CreateDirectory(modDirectory);
        var mods = new List<ChunkyModRecord>();
        var issues = new List<ChunkyModIssue>();

        foreach (var path in Directory.EnumerateFiles(modDirectory, "*.jar", SearchOption.TopDirectoryOnly).OrderBy(path => path, StringComparer.OrdinalIgnoreCase))
        {
            cancellationToken.ThrowIfCancellationRequested();
            var fileName = Path.GetFileName(path);
            try
            {
                using var archive = ZipFile.OpenRead(path);
                var metadata = archive.GetEntry("fabric.mod.json");
                if (metadata is null)
                {
                    issues.Add(new ChunkyModIssue(fileName, "No fabric.mod.json found; this file isn't recognized as a Fabric-format mod."));
                    continue;
                }
                if (metadata.Length is <= 0 or > MaximumMetadataBytes)
                {
                    issues.Add(new ChunkyModIssue(fileName, "The mod metadata is empty or exceeds the 4 MiB inspection limit."));
                    continue;
                }

                using var stream = metadata.Open();
                using var json = JsonDocument.Parse(stream, new JsonDocumentOptions { MaxDepth = 32 });
                var root = json.RootElement;
                var id = root.TryGetProperty("id", out var idValue) ? idValue.GetString() ?? "" : "";
                var version = root.TryGetProperty("version", out var versionValue) ? versionValue.ToString() : "";
                if (id.Length > 128 || !ModIdPattern.IsMatch(id))
                {
                    issues.Add(new ChunkyModIssue(fileName, "The mod metadata has a missing or invalid id."));
                    continue;
                }
                if (string.IsNullOrWhiteSpace(version))
                {
                    issues.Add(new ChunkyModIssue(fileName, "The mod metadata has no version."));
                    continue;
                }

                var environment = root.TryGetProperty("environment", out var environmentValue) ? environmentValue.GetString() ?? "*" : "*";
                var required = new List<string>();
                if (root.TryGetProperty("depends", out var dependencies) && dependencies.ValueKind == JsonValueKind.Object)
                {
                    foreach (var dependency in dependencies.EnumerateObject())
                    {
                        if (dependency.Name == "minecraft")
                        {
                            if (!MinecraftRequirementMatches(dependency.Value, profile.MinecraftVersion))
                                issues.Add(new ChunkyModIssue(fileName, $"The Minecraft requirement '{dependency.Value}' doesn't match or can't be validated for {profile.MinecraftVersion}."));
                            continue;
                        }
                        if (dependency.Name == "java") continue;
                        if (dependency.Name == "fabricloader")
                        {
                            issues.Add(new ChunkyModIssue(fileName, "This mod declares a Fabric Loader dependency. Chunky's current compatibility shim doesn't yet implement FabricLoader services or version compatibility."));
                            continue;
                        }
                        required.Add(dependency.Name);
                    }
                }
                if (environment is not ("*" or "client"))
                    issues.Add(new ChunkyModIssue(fileName, $"The mod's environment is '{environment}'; Chunky's current runtime is client-only."));

                foreach (var feature in new[] { "mixins", "accessWidener", "jars", "languageAdapters" })
                    if (root.TryGetProperty(feature, out var featureValue) && featureValue.ValueKind is not (JsonValueKind.Null or JsonValueKind.False) &&
                        !(featureValue.ValueKind == JsonValueKind.Array && featureValue.GetArrayLength() == 0) &&
                        !(featureValue.ValueKind == JsonValueKind.Object && !featureValue.EnumerateObject().Any()))
                        issues.Add(new ChunkyModIssue(fileName, $"The '{feature}' metadata feature isn't implemented by Chunky alpha."));

                var entrypoints = ReadEntrypoints(root, id, fileName, issues);
                mods.Add(new ChunkyModRecord(fileName, id, version, environment, required, entrypoints));
            }
            catch (Exception exception) when (exception is InvalidDataException or IOException or JsonException or UnauthorizedAccessException)
            {
                issues.Add(new ChunkyModIssue(fileName, $"Couldn't read this mod archive: {exception.Message}"));
            }
        }

        foreach (var group in mods.GroupBy(mod => mod.Id, StringComparer.Ordinal).Where(group => group.Count() > 1))
            foreach (var duplicate in group)
                issues.Add(new ChunkyModIssue(duplicate.FileName, $"Duplicate mod id '{group.Key}' appears more than once."));

        var installedIds = mods.Select(mod => mod.Id).ToHashSet(StringComparer.Ordinal);
        foreach (var mod in mods)
            foreach (var dependency in mod.RequiredMods.Where(id => !installedIds.Contains(id)))
                issues.Add(new ChunkyModIssue(mod.FileName, $"Required mod dependency '{dependency}' isn't present in this mods folder."));

        return new ChunkyModReport(mods, issues);
    }

    private static IReadOnlyList<ChunkyEntrypoint> ReadEntrypoints(JsonElement root, string modId, string fileName, List<ChunkyModIssue> issues)
    {
        var result = new List<ChunkyEntrypoint>();
        if (!root.TryGetProperty("entrypoints", out var entries)) return result;
        if (entries.ValueKind != JsonValueKind.Object)
        {
            issues.Add(new ChunkyModIssue(fileName, "The mod has invalid entrypoints metadata."));
            return result;
        }
        foreach (var group in entries.EnumerateObject())
        {
            if (group.Name is not ("main" or "client"))
            {
                issues.Add(new ChunkyModIssue(fileName, $"Entrypoint type '{group.Name}' isn't implemented by Chunky alpha."));
                continue;
            }
            var values = group.Value.ValueKind == JsonValueKind.Array ? group.Value.EnumerateArray().ToArray() : new[] { group.Value };
            foreach (var value in values)
            {
                var className = value.ValueKind == JsonValueKind.String ? value.GetString() :
                    value.ValueKind == JsonValueKind.Object && value.TryGetProperty("value", out var raw) ? raw.GetString() : null;
                if (value.ValueKind == JsonValueKind.Object && value.TryGetProperty("adapter", out var adapter) &&
                    (adapter.ValueKind != JsonValueKind.String || adapter.GetString() is not (null or "default")))
                {
                    issues.Add(new ChunkyModIssue(fileName, "Custom Fabric entrypoint adapters aren't implemented by Chunky alpha."));
                    continue;
                }
                if (string.IsNullOrWhiteSpace(className) || className.Length > 512 || className.Any(char.IsControl))
                {
                    issues.Add(new ChunkyModIssue(fileName, $"The {group.Name} entrypoint is not a valid class name."));
                    continue;
                }
                result.Add(new ChunkyEntrypoint(modId, group.Name, className));
            }
        }
        return result;
    }

    private static bool MinecraftRequirementMatches(JsonElement requirement, string gameVersion)
    {
        if (requirement.ValueKind == JsonValueKind.Array)
            return requirement.EnumerateArray().Any(candidate => MinecraftRequirementMatches(candidate, gameVersion));
        if (requirement.ValueKind != JsonValueKind.String) return false;
        var constraint = requirement.GetString()?.Trim();
        if (string.IsNullOrEmpty(constraint) || constraint == "*") return true;
        var candidateVersion = ParseNumericVersion(gameVersion);
        if (candidateVersion is null) return false;

        var prefix = constraint[0] is '~' or '^' ? constraint[1..] : constraint;
        var parts = prefix.Split('.');
        var actual = candidateVersion;
        var wildcardIndex = Array.FindIndex(parts, part => part is "x" or "X" or "*");
        var numericParts = wildcardIndex >= 0 ? parts[..wildcardIndex] : parts;
        var wanted = ParseNumericVersion(string.Join('.', numericParts));
        if (wanted is null) return false;
        if (constraint[0] == '~') return actual.Major == wanted.Major && (parts.Length < 2 || actual.Minor == wanted.Minor) && actual.CompareTo(wanted) >= 0;
        if (constraint[0] == '^') return actual.Major == wanted.Major && actual.CompareTo(wanted) >= 0;
        if (wildcardIndex >= 0) return actual.Major == wanted.Major && (numericParts.Length < 2 || actual.Minor == wanted.Minor);
        return actual.Equals(wanted);
    }

    private static Version? ParseNumericVersion(string value)
    {
        var stable = value.Split('-', '+')[0];
        var parts = stable.Split('.');
        if (parts.Length is < 1 or > 4 || parts.Any(part => !int.TryParse(part, out _))) return null;
        if (parts.Length > 4 || parts.Any(part => !int.TryParse(part, out _))) return null;
        var numbers = parts.Select(int.Parse).Concat(Enumerable.Repeat(0, 4 - parts.Length)).ToArray();
        return new Version(numbers[0], numbers[1], numbers[2], numbers[3]);
    }
}
