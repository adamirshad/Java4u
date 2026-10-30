using System.IO;
using System.Net.Http;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace Java4u;

public sealed class MinecraftVersionCatalog
{
    private static readonly HttpClient Client = new() { Timeout = TimeSpan.FromSeconds(12) };
    private const string ManifestUrl = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json";

    public async Task<IReadOnlyList<string>> GetRecentVersionsAsync(CancellationToken cancellationToken = default)
    {
        var manifest = await GetManifestAsync(cancellationToken);

        var releases = manifest.Versions
            .Where(version => version.Type == "release")
            .Take(40)
            .Select(version => version.Id);
        var snapshots = manifest.Versions
            .Where(version => version.Type == "snapshot")
            .Take(20)
            .Select(version => version.Id);
        return releases.Concat(snapshots).Distinct(StringComparer.Ordinal).ToArray();
    }

    public async Task<string> GetVersionMetadataUrlAsync(string versionId, CancellationToken cancellationToken = default)
    {
        var manifest = await GetManifestAsync(cancellationToken);
        return manifest.Versions.FirstOrDefault(version => version.Id == versionId)?.Url
            ?? throw new InvalidOperationException($"Minecraft version '{versionId}' wasn't found in the official manifest.");
    }

    private static async Task<VersionManifest> GetManifestAsync(CancellationToken cancellationToken)
    {
        using var response = await Client.GetAsync(ManifestUrl, HttpCompletionOption.ResponseHeadersRead, cancellationToken);
        response.EnsureSuccessStatusCode();
        await using var stream = await response.Content.ReadAsStreamAsync(cancellationToken);
        return await JsonSerializer.DeserializeAsync<VersionManifest>(stream, cancellationToken: cancellationToken)
            ?? throw new InvalidDataException("The Minecraft version list was empty.");
    }

    private sealed class VersionManifest
    {
        [JsonPropertyName("versions")]
        public List<VersionEntry> Versions { get; init; } = [];
    }

    private sealed class VersionEntry
    {
        [JsonPropertyName("id")]
        public string Id { get; init; } = "";

        [JsonPropertyName("type")]
        public string Type { get; init; } = "";

        [JsonPropertyName("url")]
        public string Url { get; init; } = "";
    }
}
