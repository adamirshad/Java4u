using System.IO;
using System.Net.Http;
using System.Security.Cryptography;
using System.Text.Json;

namespace Java4u;

public sealed record ModrinthProject(string ProjectId, string Slug, string Title, string Description, string Author, long Downloads, string IconUrl)
{
    public string Display => $"{Title}  ·  {Author}  ·  {Downloads:N0} downloads";
}

public sealed record ModrinthFile(string FileName, string Url, long Size, string Sha512, string Sha1, bool IsPrimary);
public sealed record ModrinthDependency(string? ProjectId, string? VersionId, string Type);
public sealed record ModrinthVersion(string Id, string Name, string VersionNumber, string VersionType, IReadOnlyList<string> GameVersions, IReadOnlyList<string> Loaders, IReadOnlyList<ModrinthFile> Files, IReadOnlyList<ModrinthDependency> Dependencies)
{
    public string Display => $"{Name}  ·  {VersionType}";
}

public sealed class ModrinthService
{
    private static readonly HttpClient Client = new() { Timeout = TimeSpan.FromMinutes(3) };
    private const string ApiRoot = "https://api.modrinth.com/v2";
    private const long MaximumModBytes = 256L * 1024 * 1024;

    static ModrinthService() => Client.DefaultRequestHeaders.UserAgent.ParseAdd("adamirshad/Java4u/0.5.0 (https://github.com/adamirshad/Java4u)");

    public async Task<IReadOnlyList<ModrinthProject>> SearchFabricModsAsync(string query, string minecraftVersion, CancellationToken cancellationToken = default)
    {
        var facets = JsonSerializer.Serialize(new[] { new[] { "project_type:mod" }, new[] { "categories:fabric" }, new[] { $"versions:{minecraftVersion}" } });
        var uri = $"{ApiRoot}/search?query={Uri.EscapeDataString(query.Trim())}&facets={Uri.EscapeDataString(facets)}&index=relevance&limit=40";
        using var document = await GetJsonAsync(uri, cancellationToken);
        if (!document.RootElement.TryGetProperty("hits", out var hits) || hits.ValueKind != JsonValueKind.Array) return [];
        return hits.EnumerateArray().Select(ReadProject).Where(project => project is not null).Cast<ModrinthProject>().ToArray();
    }

    public async Task<IReadOnlyList<ModrinthVersion>> GetFabricVersionsAsync(string projectId, string minecraftVersion, CancellationToken cancellationToken = default)
    {
        var uri = $"{ApiRoot}/project/{Uri.EscapeDataString(projectId)}/version?loaders={Uri.EscapeDataString("[\"fabric\"]")}&game_versions={Uri.EscapeDataString(JsonSerializer.Serialize(new[] { minecraftVersion }))}&include_changelog=false";
        using var document = await GetJsonAsync(uri, cancellationToken);
        if (document.RootElement.ValueKind != JsonValueKind.Array) return [];
        return document.RootElement.EnumerateArray().Select(ReadVersion).Where(version => version is not null).Cast<ModrinthVersion>().ToArray();
    }

    public async Task InstallAsync(InstanceProfile profile, ModrinthVersion selected, IProgress<string>? progress = null, CancellationToken cancellationToken = default)
    {
        if (!profile.IsInstalled) throw new InvalidOperationException("Install this instance's Minecraft version before adding mods.");
        if (!string.Equals(profile.Loader, "Chunky", StringComparison.Ordinal)) throw new InvalidOperationException("Choose a Chunky instance before adding Fabric-compatible mods.");
        var instanceRoot = Path.GetFullPath(profile.InstanceDirectory);
        var java4uRoot = Path.GetFullPath(Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Java4u", "instances"));
        if (profile.Id == Guid.Empty || !instanceRoot.StartsWith(java4uRoot + Path.DirectorySeparatorChar, StringComparison.OrdinalIgnoreCase))
            throw new InvalidOperationException("The selected instance folder isn't inside Java4u's data directory.");
        var installRoot = Path.GetFullPath(Path.Combine(instanceRoot, ".minecraft"));
        var destination = Path.GetFullPath(Path.Combine(installRoot, "mods"));
        if (!destination.StartsWith(installRoot + Path.DirectorySeparatorChar, StringComparison.OrdinalIgnoreCase)) throw new InvalidOperationException("The mod folder path isn't inside this instance.");
        Directory.CreateDirectory(destination);

        var pending = new Queue<ModrinthVersion>();
        var seenVersions = new HashSet<string>(StringComparer.Ordinal);
        pending.Enqueue(selected);
        var installedCount = 0;
        while (pending.TryDequeue(out var version))
        {
            cancellationToken.ThrowIfCancellationRequested();
            if (!seenVersions.Add(version.Id)) continue;
            if (seenVersions.Count > 32) throw new InvalidDataException("This mod has more than 32 required dependencies; stop to review its dependency chain.");
            if (!version.Loaders.Contains("fabric", StringComparer.Ordinal) || !version.GameVersions.Contains(profile.MinecraftVersion, StringComparer.Ordinal))
                throw new InvalidDataException($"Modrinth dependency '{version.Name}' doesn't support Fabric on Minecraft {profile.MinecraftVersion}.");

            foreach (var dependency in version.Dependencies.Where(item => item.Type == "required"))
            {
                var requiredVersion = await ResolveDependencyAsync(dependency, profile.MinecraftVersion, cancellationToken);
                if (requiredVersion is not null && !seenVersions.Contains(requiredVersion.Id)) pending.Enqueue(requiredVersion);
            }

            var file = version.Files.FirstOrDefault(item => item.IsPrimary) ?? version.Files.FirstOrDefault();
            if (file is null) throw new InvalidDataException($"Mod version '{version.Name}' has no downloadable file.");
            var finalName = Path.GetFileName(file.FileName);
            if (!string.Equals(finalName, file.FileName, StringComparison.Ordinal) || !finalName.EndsWith(".jar", StringComparison.OrdinalIgnoreCase) || finalName.IndexOfAny(Path.GetInvalidFileNameChars()) >= 0)
                throw new InvalidDataException("Modrinth returned an invalid mod file name.");
            var target = Path.Combine(destination, finalName);
            if (!await IsVerifiedAsync(target, file, cancellationToken))
            {
                progress?.Report($"Downloading {finalName}…");
                await DownloadVerifiedAsync(file, target, cancellationToken);
                installedCount++;
            }
            else progress?.Report($"Already installed: {finalName}");
        }
        progress?.Report($"Added or verified {installedCount} mod file(s). Chunky runtime support is still required before these mods can launch.");
    }

    private static async Task<ModrinthVersion?> ResolveDependencyAsync(ModrinthDependency dependency, string minecraftVersion, CancellationToken cancellationToken)
    {
        if (!string.IsNullOrWhiteSpace(dependency.VersionId))
        {
            using var document = await GetJsonAsync($"{ApiRoot}/version/{Uri.EscapeDataString(dependency.VersionId)}", cancellationToken);
            return ReadVersion(document.RootElement);
        }
        if (string.IsNullOrWhiteSpace(dependency.ProjectId)) return null;
        var versions = await new ModrinthService().GetFabricVersionsAsync(dependency.ProjectId, minecraftVersion, cancellationToken);
        return versions.FirstOrDefault(version => version.VersionType == "release") ?? versions.FirstOrDefault();
    }

    private static ModrinthProject? ReadProject(JsonElement value)
    {
        if (!value.TryGetProperty("project_id", out var id) || string.IsNullOrWhiteSpace(id.GetString())) return null;
        return new ModrinthProject(id.GetString()!, ReadString(value, "slug"), ReadString(value, "title"), ReadString(value, "description"), ReadString(value, "author"), ReadLong(value, "downloads"), ReadString(value, "icon_url"));
    }

    private static ModrinthVersion? ReadVersion(JsonElement value)
    {
        var id = ReadString(value, "id");
        if (id.Length == 0) return null;
        var gameVersions = ReadStringArray(value, "game_versions");
        var loaders = ReadStringArray(value, "loaders");
        var files = new List<ModrinthFile>();
        if (value.TryGetProperty("files", out var filesJson) && filesJson.ValueKind == JsonValueKind.Array)
        {
            foreach (var file in filesJson.EnumerateArray())
            {
                if (!Uri.TryCreate(ReadString(file, "url"), UriKind.Absolute, out var uri) || uri.Scheme != Uri.UriSchemeHttps || !uri.Host.Equals("cdn.modrinth.com", StringComparison.OrdinalIgnoreCase))
                    throw new InvalidDataException("Modrinth returned a download URL outside its official HTTPS CDN.");
                var hashes = file.TryGetProperty("hashes", out var hashesJson) ? hashesJson : default;
                files.Add(new ModrinthFile(ReadString(file, "filename"), uri.AbsoluteUri, ReadLong(file, "size"), ReadString(hashes, "sha512"), ReadString(hashes, "sha1"), file.TryGetProperty("primary", out var primary) && primary.ValueKind == JsonValueKind.True));
            }
        }
        var dependencies = new List<ModrinthDependency>();
        if (value.TryGetProperty("dependencies", out var dependenciesJson) && dependenciesJson.ValueKind == JsonValueKind.Array)
            foreach (var dependency in dependenciesJson.EnumerateArray())
                dependencies.Add(new ModrinthDependency(ReadNullableString(dependency, "project_id"), ReadNullableString(dependency, "version_id"), ReadString(dependency, "dependency_type")));
        return new ModrinthVersion(id, ReadString(value, "name"), ReadString(value, "version_number"), ReadString(value, "version_type"), gameVersions, loaders, files, dependencies);
    }

    private static async Task<JsonDocument> GetJsonAsync(string uri, CancellationToken cancellationToken)
    {
        using var response = await Client.GetAsync(uri, HttpCompletionOption.ResponseHeadersRead, cancellationToken);
        response.EnsureSuccessStatusCode();
        if (response.RequestMessage?.RequestUri is not { Scheme: "https", Host: "api.modrinth.com" })
            throw new InvalidDataException("Modrinth metadata redirected outside its official HTTPS API.");
        await using var stream = await response.Content.ReadAsStreamAsync(cancellationToken);
        if (response.Content.Headers.ContentLength is > 8 * 1024 * 1024) throw new InvalidDataException("Modrinth returned more metadata than Java4u expected.");
        using var buffer = new MemoryStream();
        var chunk = new byte[32 * 1024];
        while (true)
        {
            var count = await stream.ReadAsync(chunk.AsMemory(0, chunk.Length), cancellationToken);
            if (count == 0) break;
            if (buffer.Length + count > 8 * 1024 * 1024) throw new InvalidDataException("Modrinth returned more metadata than Java4u expected.");
            buffer.Write(chunk, 0, count);
        }
        buffer.Position = 0;
        return await JsonDocument.ParseAsync(buffer, new JsonDocumentOptions { MaxDepth = 48 }, cancellationToken);
    }

    private static async Task<bool> IsVerifiedAsync(string path, ModrinthFile file, CancellationToken cancellationToken)
    {
        if (!File.Exists(path)) return false;
        await using var existing = File.OpenRead(path);
        if (file.Size >= 0 && existing.Length != file.Size) return false;
        if (file.Sha512.Length == 128 && file.Sha512.All(Uri.IsHexDigit))
            return Convert.ToHexString(await SHA512.HashDataAsync(existing, cancellationToken)).Equals(file.Sha512, StringComparison.OrdinalIgnoreCase);
        existing.Position = 0;
        return file.Sha1.Length == 40 && file.Sha1.All(Uri.IsHexDigit) && Convert.ToHexString(await SHA1.HashDataAsync(existing, cancellationToken)).Equals(file.Sha1, StringComparison.OrdinalIgnoreCase);
    }

    private static async Task DownloadVerifiedAsync(ModrinthFile file, string destination, CancellationToken cancellationToken)
    {
        if (file.Size is < 0 or > MaximumModBytes) throw new InvalidDataException("The mod file is larger than Java4u's 256 MiB safety limit.");
        var useSha512 = file.Sha512.Length == 128 && file.Sha512.All(Uri.IsHexDigit);
        if (!useSha512 && !(file.Sha1.Length == 40 && file.Sha1.All(Uri.IsHexDigit))) throw new InvalidDataException("Modrinth didn't provide a valid SHA-512 or SHA-1 checksum.");
        var temp = destination + ".download-" + Guid.NewGuid().ToString("N");
        try
        {
            using var response = await Client.GetAsync(file.Url, HttpCompletionOption.ResponseHeadersRead, cancellationToken);
            response.EnsureSuccessStatusCode();
            var finalUri = response.RequestMessage?.RequestUri;
            if (finalUri is null || finalUri.Scheme != Uri.UriSchemeHttps || !finalUri.Host.Equals("cdn.modrinth.com", StringComparison.OrdinalIgnoreCase))
                throw new InvalidDataException("The mod download redirected outside Modrinth's HTTPS CDN.");
            await using var input = await response.Content.ReadAsStreamAsync(cancellationToken);
            await using var output = new FileStream(temp, FileMode.CreateNew, FileAccess.Write, FileShare.None, 128 * 1024, useAsync: true);
            using var hash = IncrementalHash.CreateHash(useSha512 ? HashAlgorithmName.SHA512 : HashAlgorithmName.SHA1);
            var buffer = new byte[128 * 1024];
            long received = 0;
            while (true)
            {
                var read = await input.ReadAsync(buffer.AsMemory(0, buffer.Length), cancellationToken);
                if (read == 0) break;
                received += read;
                if (received > MaximumModBytes || file.Size >= 0 && received > file.Size) throw new InvalidDataException("The mod download exceeded its published size.");
                hash.AppendData(buffer, 0, read);
                await output.WriteAsync(buffer.AsMemory(0, read), cancellationToken);
            }
            await output.FlushAsync(cancellationToken);
            var expectedHash = useSha512 ? file.Sha512 : file.Sha1;
            if (file.Size >= 0 && received != file.Size || !Convert.ToHexString(hash.GetHashAndReset()).Equals(expectedHash, StringComparison.OrdinalIgnoreCase))
                throw new InvalidDataException("The mod download failed Modrinth's size or checksum verification.");
            File.Move(temp, destination, overwrite: true);
        }
        finally { if (File.Exists(temp)) File.Delete(temp); }
    }

    private static string ReadString(JsonElement element, string property)
        => element.ValueKind == JsonValueKind.Object && element.TryGetProperty(property, out var value) && value.ValueKind == JsonValueKind.String ? value.GetString() ?? "" : "";
    private static string? ReadNullableString(JsonElement element, string property)
        => element.ValueKind == JsonValueKind.Object && element.TryGetProperty(property, out var value) && value.ValueKind == JsonValueKind.String ? value.GetString() : null;
    private static long ReadLong(JsonElement element, string property)
        => element.ValueKind == JsonValueKind.Object && element.TryGetProperty(property, out var value) && value.TryGetInt64(out var number) ? number : 0;
    private static string[] ReadStringArray(JsonElement element, string property)
        => element.ValueKind == JsonValueKind.Object && element.TryGetProperty(property, out var value) && value.ValueKind == JsonValueKind.Array ? value.EnumerateArray().Where(item => item.ValueKind == JsonValueKind.String).Select(item => item.GetString() ?? "").ToArray() : [];
}
