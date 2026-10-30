using System.IO;
using System.IO.Compression;
using System.Net.Http;
using System.Runtime.InteropServices;
using System.Security.Cryptography;
using System.Text.Json;
using System.Text.RegularExpressions;

namespace Java4u;

public sealed class MinecraftCoreInstaller
{
    private static readonly HttpClient Client = new() { Timeout = Timeout.InfiniteTimeSpan };
    private const long MaximumMetadataBytes = 32 * 1024 * 1024;
    private const long MaximumFileBytes = 2L * 1024 * 1024 * 1024;
    private const int MaximumConcurrentDownloads = 6;
    private static readonly TimeSpan RequestTimeout = TimeSpan.FromMinutes(4);
    private readonly MinecraftVersionCatalog _catalog = new();

    public async Task InstallAsync(InstanceProfile profile, IProgress<string>? progress = null, CancellationToken cancellationToken = default)
    {
        var versionId = profile.MinecraftVersion;
        if (string.IsNullOrWhiteSpace(versionId)) throw new InvalidOperationException("Choose a Minecraft version first.");

        var instanceRoot = Path.GetFullPath(profile.InstanceDirectory);
        var appInstancesRoot = Path.GetFullPath(Path.Combine(
            Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Java4u", "instances"));
        if (!instanceRoot.StartsWith(appInstancesRoot + Path.DirectorySeparatorChar, StringComparison.OrdinalIgnoreCase))
            throw new InvalidOperationException("The selected instance folder isn't inside Java4u's data directory.");

        var versionFolder = Path.GetFullPath(Path.Combine(instanceRoot, ".minecraft", "versions", versionId));
        var minecraftRoot = Path.GetFullPath(Path.Combine(instanceRoot, ".minecraft"));
        if (!versionFolder.StartsWith(minecraftRoot + Path.DirectorySeparatorChar, StringComparison.OrdinalIgnoreCase))
            throw new InvalidOperationException("The selected version path is invalid.");

        progress?.Report("Checking Mojang's official version manifest…");
        cancellationToken.ThrowIfCancellationRequested();
        var metadataUrl = await _catalog.GetVersionMetadataUrlAsync(versionId, cancellationToken);
        EnsureDownloadUrl(metadataUrl);
        using var versionMetadata = await GetJsonAsync(metadataUrl, MaximumMetadataBytes, cancellationToken);
        var root = versionMetadata.RootElement;
        if (!root.TryGetProperty("id", out var idElement) || idElement.GetString() != versionId)
            throw new InvalidDataException("The downloaded version metadata didn't match the selected version.");
        if (!root.TryGetProperty("downloads", out var downloads) || !downloads.TryGetProperty("client", out var client))
            throw new InvalidDataException("The official manifest doesn't provide a client download for this version.");
        var requiredJavaMajor = ResolveRequiredJavaMajor(root, versionId);
        if (requiredJavaMajor is < 0 or > 100) throw new InvalidDataException("The version metadata contains an invalid Java runtime requirement.");
        var clientUrl = client.GetProperty("url").GetString() ?? "";
        var clientSha1 = client.GetProperty("sha1").GetString() ?? "";
        EnsureDownloadUrl(clientUrl);
        var clientPlan = CreateDownloadPlan(clientUrl, clientSha1, GetOptionalInt64(client, "size"), Path.Combine(versionFolder, $"{versionId}.jar"), "client");

        Directory.CreateDirectory(versionFolder);
        var metadataPath = Path.Combine(versionFolder, $"{versionId}.json");
        await WriteJsonAtomicallyAsync(metadataPath, versionMetadata, cancellationToken);

        var files = new Dictionary<string, DownloadPlan>(StringComparer.OrdinalIgnoreCase) { [clientPlan.Destination] = clientPlan };
        var assetsRoot = Path.Combine(minecraftRoot, "assets");
        var nativeArchives = new List<NativeArchivePlan>();
        AddLibraryDownloads(root, Path.Combine(minecraftRoot, "libraries"), files, nativeArchives);
        await AddAssetDownloadsAsync(root, assetsRoot, files, progress, cancellationToken);
        AddLoggingConfigDownload(root, assetsRoot, files);

        var plans = files.Values.ToArray();
        progress?.Report($"Preparing {plans.Length} verified game files…");
        var completed = 0;
        using var gate = new SemaphoreSlim(MaximumConcurrentDownloads);
        var progressGate = new object();
        var lastProgress = DateTime.UtcNow;
        using var installCancellation = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        var tasks = plans.Select(async plan =>
        {
            await gate.WaitAsync(installCancellation.Token);
            try
            {
                await DownloadVerifiedAsync(plan, installCancellation.Token);
                var count = Interlocked.Increment(ref completed);
                var now = DateTime.UtcNow;
                lock (progressGate)
                {
                    if (count == plans.Length || now - lastProgress >= TimeSpan.FromMilliseconds(250))
                    {
                        progress?.Report($"Verified {count:N0} of {plans.Length:N0} game files…");
                        lastProgress = now;
                    }
                }
            }
            catch
            {
                installCancellation.Cancel();
                throw;
            }
            finally { gate.Release(); }
        });
        await Task.WhenAll(tasks);
        progress?.Report("Assembling Windows native libraries…");
        await ExtractNativesAsync(nativeArchives, Path.Combine(versionFolder, "natives"), cancellationToken);
        profile.RequiredJavaMajorVersion = requiredJavaMajor;
        profile.InstalledMinecraftVersion = versionId;
        profile.InstalledUtc = DateTime.UtcNow;
        progress?.Report($"Minecraft {versionId} files and Windows natives are ready.");
    }

    private static int ResolveRequiredJavaMajor(JsonElement version, string versionId)
    {
        if (version.TryGetProperty("javaVersion", out var javaVersion) && javaVersion.TryGetProperty("majorVersion", out var majorVersion) && majorVersion.TryGetInt32(out var parsedMajor))
        {
            if (parsedMajor is < 1 or > 100) throw new InvalidDataException("The version metadata contains an invalid Java runtime requirement.");
            return parsedMajor;
        }

        // Early Mojang version records predate the javaVersion field. Use the
        // documented runtime transition points to keep those releases launchable.
        var match = Regex.Match(versionId, @"^(?<major>\d+)\.(?<minor>\d+)(?:\.(?<patch>\d+))?");
        if (!match.Success || !int.TryParse(match.Groups["major"].Value, out var gameMajor) || !int.TryParse(match.Groups["minor"].Value, out var gameMinor))
            throw new InvalidDataException("Java4u couldn't determine the Java runtime required by this Minecraft version.");

        if (gameMajor > 1 || gameMinor >= 21) return 21;
        if (gameMinor >= 20 && int.TryParse(match.Groups["patch"].Value, out var patch) && patch >= 5) return 21;
        if (gameMinor >= 18) return 17;
        if (gameMinor == 17) return 16;
        return 8;
    }

    private static void AddLoggingConfigDownload(JsonElement version, string assetsRoot, Dictionary<string, DownloadPlan> files)
    {
        if (!version.TryGetProperty("logging", out var logging) || !logging.TryGetProperty("client", out var client) || !client.TryGetProperty("file", out var file)) return;
        var id = file.GetProperty("id").GetString() ?? throw new InvalidDataException("The logging config is missing its id.");
        if (id.IndexOfAny(Path.GetInvalidFileNameChars()) >= 0 || id.Contains("..", StringComparison.Ordinal)) throw new InvalidDataException("The logging config id is invalid.");
        var destination = ResolveSafePath(Path.Combine(assetsRoot, "log_configs"), id);
        var url = file.GetProperty("url").GetString() ?? "";
        var hash = file.GetProperty("sha1").GetString() ?? "";
        files.TryAdd(destination, CreateDownloadPlan(url, hash, GetOptionalInt64(file, "size"), destination, "logging config"));
    }

    private static void AddLibraryDownloads(JsonElement version, string libraryRoot, Dictionary<string, DownloadPlan> files, List<NativeArchivePlan> nativeArchives)
    {
        if (!version.TryGetProperty("libraries", out var libraries) || libraries.ValueKind != JsonValueKind.Array) return;
        foreach (var library in libraries.EnumerateArray())
        {
            if (!RulesAllow(library)) continue;
            if (!library.TryGetProperty("downloads", out var downloads)) continue;
            if (downloads.TryGetProperty("artifact", out var artifact)) AddArtifact(artifact, libraryRoot, files, "library");

            if (!library.TryGetProperty("natives", out var natives) || !natives.TryGetProperty("windows", out var windowsNative)) continue;
            var classifier = windowsNative.GetString()?.Replace("${arch}", Environment.Is64BitOperatingSystem ? "64" : "32", StringComparison.Ordinal);
            if (string.IsNullOrWhiteSpace(classifier) || !downloads.TryGetProperty("classifiers", out var classifiers) || !classifiers.TryGetProperty(classifier, out var nativeArtifact)) continue;
            AddArtifact(nativeArtifact, libraryRoot, files, "native library");
            var relativePath = nativeArtifact.GetProperty("path").GetString() ?? throw new InvalidDataException("A native library download is missing its path.");
            var archivePath = ResolveSafePath(libraryRoot, relativePath);
            var excludedPrefixes = new List<string>();
            if (library.TryGetProperty("extract", out var extract) && extract.TryGetProperty("exclude", out var exclude) && exclude.ValueKind == JsonValueKind.Array)
                excludedPrefixes.AddRange(exclude.EnumerateArray().Select(value => value.GetString()).OfType<string>().Where(value => !string.IsNullOrWhiteSpace(value)));
            nativeArchives.Add(new NativeArchivePlan(archivePath, excludedPrefixes));
        }
    }

    private static async Task ExtractNativesAsync(IReadOnlyList<NativeArchivePlan> archives, string destination, CancellationToken cancellationToken)
    {
        var staging = destination + ".tmp-" + Guid.NewGuid().ToString("N");
        var backup = destination + ".old-" + Guid.NewGuid().ToString("N");
        Directory.CreateDirectory(staging);
        long totalBytes = 0;
        try
        {
            foreach (var archivePlan in archives)
            {
                cancellationToken.ThrowIfCancellationRequested();
                await using var archiveStream = File.OpenRead(archivePlan.Path);
                using var archive = new ZipArchive(archiveStream, ZipArchiveMode.Read);
                foreach (var entry in archive.Entries)
                {
                    cancellationToken.ThrowIfCancellationRequested();
                    var normalized = entry.FullName.Replace('\\', '/');
                    if (string.IsNullOrEmpty(normalized) || normalized.EndsWith('/')) continue;
                    if (normalized.Split('/').Any(segment => segment is "" or "." or "..") || Path.IsPathRooted(normalized))
                        throw new InvalidDataException("A native archive contains an unsafe file path.");
                    if (normalized.StartsWith("META-INF/", StringComparison.OrdinalIgnoreCase) || normalized.Equals("META-INF", StringComparison.OrdinalIgnoreCase)) continue;
                    if (archivePlan.ExcludedPrefixes.Any(prefix => normalized.StartsWith(prefix.TrimEnd('/') + "/", StringComparison.OrdinalIgnoreCase) || normalized.Equals(prefix.TrimEnd('/'), StringComparison.OrdinalIgnoreCase))) continue;
                    if (entry.Length > 128L * 1024 * 1024 || totalBytes + entry.Length > 512L * 1024 * 1024)
                        throw new InvalidDataException("The native libraries exceed Java4u's extraction safety limit.");
                    var outputPath = ResolveSafePath(staging, normalized.Replace('/', Path.DirectorySeparatorChar));
                    Directory.CreateDirectory(Path.GetDirectoryName(outputPath)!);
                    await using var input = entry.Open();
                    await using var output = new FileStream(outputPath, FileMode.Create, FileAccess.Write, FileShare.None, 64 * 1024, useAsync: true);
                    await input.CopyToAsync(output, cancellationToken);
                    totalBytes += entry.Length;
                }
            }

            if (Directory.Exists(destination)) Directory.Move(destination, backup);
            try { Directory.Move(staging, destination); }
            catch
            {
                if (Directory.Exists(backup) && !Directory.Exists(destination)) Directory.Move(backup, destination);
                throw;
            }
            if (Directory.Exists(backup)) Directory.Delete(backup, recursive: true);
        }
        finally
        {
            if (Directory.Exists(staging)) Directory.Delete(staging, recursive: true);
        }
    }

    private static void AddArtifact(JsonElement artifact, string root, Dictionary<string, DownloadPlan> files, string label)
    {
        var relativePath = artifact.GetProperty("path").GetString() ?? throw new InvalidDataException("A library download is missing its path.");
        var destination = ResolveSafePath(root, relativePath);
        var url = artifact.GetProperty("url").GetString() ?? "";
        EnsureDownloadUrl(url);
        var sha1 = artifact.TryGetProperty("sha1", out var hash) ? hash.GetString() ?? "" : "";
        var plan = CreateDownloadPlan(url, sha1, GetOptionalInt64(artifact, "size"), destination, label);
        files.TryAdd(plan.Destination, plan);
    }

    private static async Task AddAssetDownloadsAsync(JsonElement version, string assetsRoot, Dictionary<string, DownloadPlan> files, IProgress<string>? progress, CancellationToken cancellationToken)
    {
        if (!version.TryGetProperty("assetIndex", out var assetIndex)) return;
        var indexId = assetIndex.GetProperty("id").GetString();
        if (string.IsNullOrWhiteSpace(indexId) || indexId.IndexOfAny(Path.GetInvalidFileNameChars()) >= 0 || indexId.Contains("..", StringComparison.Ordinal))
            throw new InvalidDataException("The version metadata contains an invalid asset index id.");
        var indexUrl = assetIndex.GetProperty("url").GetString() ?? "";
        EnsureDownloadUrl(indexUrl);
        var indexPath = Path.Combine(assetsRoot, "indexes", indexId + ".json");
        var indexPlan = CreateDownloadPlan(indexUrl, assetIndex.GetProperty("sha1").GetString() ?? "", GetOptionalInt64(assetIndex, "size"), indexPath, "asset index");
        progress?.Report("Checking the official asset index…");
        await DownloadVerifiedAsync(indexPlan, cancellationToken);

        using var index = JsonDocument.Parse(await File.ReadAllBytesAsync(indexPath, cancellationToken));
        if (!index.RootElement.TryGetProperty("objects", out var objects) || objects.ValueKind != JsonValueKind.Object) return;
        foreach (var asset in objects.EnumerateObject())
        {
            var hash = asset.Value.GetProperty("hash").GetString() ?? "";
            if (hash.Length != 40 || !hash.All(Uri.IsHexDigit)) throw new InvalidDataException("The asset index contains an invalid SHA-1 hash.");
            var relative = Path.Combine("objects", hash[..2], hash);
            var destination = ResolveSafePath(assetsRoot, relative);
            var url = $"https://resources.download.minecraft.net/{hash[..2]}/{hash}";
            files.TryAdd(destination, CreateDownloadPlan(url, hash, GetOptionalInt64(asset.Value, "size"), destination, "asset"));
        }
    }

    internal static bool RulesAllow(JsonElement item)
    {
        if (!item.TryGetProperty("rules", out var rules) || rules.ValueKind != JsonValueKind.Array) return true;
        var allowed = false;
        foreach (var rule in rules.EnumerateArray())
        {
            if (RuleMatches(rule)) allowed = rule.GetProperty("action").GetString() == "allow";
        }
        return allowed;
    }

    private static bool RuleMatches(JsonElement rule)
    {
        if (rule.TryGetProperty("os", out var os))
        {
            if (os.TryGetProperty("name", out var name) && name.GetString() != "windows") return false;
            if (os.TryGetProperty("arch", out var arch))
            {
                var currentArch = RuntimeInformation.OSArchitecture == Architecture.X64 ? "x86_64" : RuntimeInformation.OSArchitecture == Architecture.X86 ? "x86" : RuntimeInformation.OSArchitecture.ToString().ToLowerInvariant();
                if (!Regex.IsMatch(currentArch, arch.GetString() ?? "(?!)")) return false;
            }
            if (os.TryGetProperty("version", out var version) && !Regex.IsMatch(Environment.OSVersion.Version.ToString(), version.GetString() ?? "(?!)")) return false;
        }
        if (rule.TryGetProperty("features", out var features))
        {
            foreach (var feature in features.EnumerateObject())
            {
                var defaultValue = feature.Name is "is_demo_user" or "has_custom_resolution" or "has_quick_plays_support" or "is_quick_play_singleplayer" or "is_quick_play_multiplayer" or "is_quick_play_realms" ? false : (bool?)null;
                if (defaultValue is null || feature.Value.ValueKind is not (JsonValueKind.True or JsonValueKind.False) || feature.Value.GetBoolean() != defaultValue.GetValueOrDefault()) return false;
            }
        }
        return true;
    }

    private static DownloadPlan CreateDownloadPlan(string url, string sha1, long? size, string destination, string label)
    {
        EnsureDownloadUrl(url);
        if (sha1.Length != 40 || !sha1.All(Uri.IsHexDigit)) throw new InvalidDataException($"The official {label} entry didn't include a valid SHA-1 hash.");
        if (size is < 0) throw new InvalidDataException($"The official {label} entry contains an invalid size.");
        return new DownloadPlan(url, sha1, size, destination, label);
    }

    private static long? GetOptionalInt64(JsonElement element, string propertyName)
        => element.TryGetProperty(propertyName, out var value) && value.TryGetInt64(out var number) ? number : null;

    private static string ResolveSafePath(string root, string relativePath)
    {
        if (Path.IsPathRooted(relativePath) || relativePath.Contains("..", StringComparison.Ordinal))
            throw new InvalidDataException("The official manifest contains an unsafe file path.");
        var rootPath = Path.GetFullPath(root);
        var target = Path.GetFullPath(Path.Combine(rootPath, relativePath));
        if (!target.StartsWith(rootPath + Path.DirectorySeparatorChar, StringComparison.OrdinalIgnoreCase))
            throw new InvalidDataException("A game file path escaped its install folder.");
        return target;
    }

    private static async Task<JsonDocument> GetJsonAsync(string url, long maximumBytes, CancellationToken cancellationToken)
    {
        EnsureDownloadUrl(url);
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        timeout.CancelAfter(RequestTimeout);
        using var response = await Client.GetAsync(url, HttpCompletionOption.ResponseHeadersRead, timeout.Token);
        response.EnsureSuccessStatusCode();
        EnsureDownloadUrl(response.RequestMessage?.RequestUri?.AbsoluteUri ?? url);
        if (response.Content.Headers.ContentLength is long contentLength && contentLength > maximumBytes)
            throw new InvalidDataException("Mojang metadata exceeded the expected size limit.");
        await using var stream = await response.Content.ReadAsStreamAsync(timeout.Token);
        using var buffer = new MemoryStream();
        var chunk = new byte[32 * 1024];
        while (true)
        {
            var read = await stream.ReadAsync(chunk, timeout.Token);
            if (read == 0) break;
            if (buffer.Length + read > maximumBytes) throw new InvalidDataException("Mojang metadata exceeded the expected size limit.");
            buffer.Write(chunk, 0, read);
        }
        return JsonDocument.Parse(buffer.ToArray());
    }

    private static async Task WriteJsonAtomicallyAsync<T>(string path, T value, CancellationToken cancellationToken)
    {
        var tempPath = path + ".tmp";
        try
        {
            await using (var stream = new FileStream(tempPath, FileMode.Create, FileAccess.Write, FileShare.None, 65536, useAsync: true))
                await JsonSerializer.SerializeAsync(stream, value, cancellationToken: cancellationToken);
            File.Move(tempPath, path, overwrite: true);
        }
        finally
        {
            if (File.Exists(tempPath)) File.Delete(tempPath);
        }
    }

    private static async Task DownloadVerifiedAsync(DownloadPlan download, CancellationToken cancellationToken)
    {
        if (File.Exists(download.Destination))
        {
            await using var existing = File.OpenRead(download.Destination);
            var existingHash = Convert.ToHexString(await SHA1.HashDataAsync(existing, cancellationToken));
            if (existingHash.Equals(download.Sha1, StringComparison.OrdinalIgnoreCase)) return;
        }

        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        timeout.CancelAfter(RequestTimeout);
        using var request = new HttpRequestMessage(HttpMethod.Get, download.Url);
        using var response = await Client.SendAsync(request, HttpCompletionOption.ResponseHeadersRead, timeout.Token);
        response.EnsureSuccessStatusCode();
        EnsureDownloadUrl(response.RequestMessage?.RequestUri?.AbsoluteUri ?? download.Url);
        if (response.Content.Headers.ContentLength is long contentLength && (contentLength > MaximumFileBytes || download.Size is long expected && contentLength != expected))
            throw new InvalidDataException($"The downloaded {download.Label} has an unexpected file size.");
        var tempPath = download.Destination + ".download";
        try
        {
            Directory.CreateDirectory(Path.GetDirectoryName(download.Destination)!);
            await using var input = await response.Content.ReadAsStreamAsync(timeout.Token);
            await using var output = new FileStream(tempPath, FileMode.Create, FileAccess.Write, FileShare.None, 128 * 1024, useAsync: true);
            using var hash = IncrementalHash.CreateHash(HashAlgorithmName.SHA1);
            var buffer = new byte[128 * 1024];
            long received = 0;
            while (true)
            {
                var read = await input.ReadAsync(buffer, timeout.Token);
                if (read == 0) break;
                await output.WriteAsync(buffer.AsMemory(0, read), timeout.Token);
                hash.AppendData(buffer, 0, read);
                received += read;
                if (received > MaximumFileBytes || download.Size is long expectedLimit && received > expectedLimit)
                    throw new InvalidDataException($"The downloaded {download.Label} exceeded its published size limit.");
            }
            await output.FlushAsync(timeout.Token);
            var actualHash = Convert.ToHexString(hash.GetHashAndReset());
            if (!actualHash.Equals(download.Sha1, StringComparison.OrdinalIgnoreCase) || download.Size is long expectedSize && received != expectedSize)
                throw new InvalidDataException($"The downloaded {download.Label} failed Mojang's integrity check. The incomplete file was discarded.");
            File.Move(tempPath, download.Destination, overwrite: true);
        }
        finally
        {
            if (File.Exists(tempPath)) File.Delete(tempPath);
        }
    }

    private static void EnsureDownloadUrl(string value)
    {
        if (!Uri.TryCreate(value, UriKind.Absolute, out var uri) || uri.Scheme != Uri.UriSchemeHttps ||
            !(uri.Host.Equals("mojang.com", StringComparison.OrdinalIgnoreCase) || uri.Host.EndsWith(".mojang.com", StringComparison.OrdinalIgnoreCase) ||
              uri.Host.Equals("minecraft.net", StringComparison.OrdinalIgnoreCase) || uri.Host.EndsWith(".minecraft.net", StringComparison.OrdinalIgnoreCase)))
            throw new InvalidDataException("The official manifest contains a download URL outside Mojang's Minecraft HTTPS domains.");
    }

    private sealed record DownloadPlan(string Url, string Sha1, long? Size, string Destination, string Label);
    private sealed record NativeArchivePlan(string Path, IReadOnlyList<string> ExcludedPrefixes);
}
