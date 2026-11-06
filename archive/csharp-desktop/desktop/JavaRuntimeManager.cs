using System.IO;
using System.IO.Compression;
using System.Net.Http;
using System.Security.Cryptography;
using System.Text.Json;
using System.Text.RegularExpressions;

namespace Java4u;

public sealed class JavaRuntimeManager
{
    private static readonly HttpClient Client = new() { Timeout = Timeout.InfiniteTimeSpan };
    private static readonly TimeSpan RequestTimeout = TimeSpan.FromMinutes(8);
    private const long MaximumArchiveBytes = 1024L * 1024 * 1024;
    private const long MaximumExtractedBytes = 2L * 1024 * 1024 * 1024;

    public async Task<string> InstallTemurinAsync(int majorVersion, IProgress<string>? progress = null, CancellationToken cancellationToken = default)
    {
        if (majorVersion is < 8 or > 100) throw new InvalidOperationException("Java4u couldn't identify a supported Java version for this Minecraft release.");
        var appRoot = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Java4u");
        var runtimesRoot = Path.GetFullPath(Path.Combine(appRoot, "runtimes"));
        var destination = Path.Combine(runtimesRoot, $"temurin-{majorVersion}-windows-x64");
        var existing = FindJavaExecutable(destination);
        if (existing is not null)
        {
            var current = await new JavaRuntimeInspector().InspectAsync(new InstanceProfile { JavaExecutablePath = existing, RequiredJavaMajorVersion = majorVersion }, cancellationToken);
            if (current.IsCompatible) return existing;
        }

        Directory.CreateDirectory(runtimesRoot);
        var metadataUri = $"https://api.adoptium.net/v3/assets/latest/{majorVersion}/hotspot?architecture=x64&image_type=jre&os=windows&vendor=eclipse";
        progress?.Report($"Finding Temurin Java {majorVersion} for Windows x64…");
        using var metadataTimeout = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        metadataTimeout.CancelAfter(TimeSpan.FromSeconds(30));
        using var metadataResponse = await Client.GetAsync(metadataUri, HttpCompletionOption.ResponseHeadersRead, metadataTimeout.Token);
        if (metadataResponse.StatusCode == System.Net.HttpStatusCode.NotFound)
            throw new InvalidOperationException($"Eclipse Adoptium doesn't currently publish a Java {majorVersion} JRE for Windows x64.");
        metadataResponse.EnsureSuccessStatusCode();
        if (metadataResponse.RequestMessage?.RequestUri is not { Scheme: "https", Host: "api.adoptium.net" })
            throw new InvalidDataException("The Java runtime metadata redirected outside Eclipse Adoptium's official API.");
        if (metadataResponse.Content.Headers.ContentLength is > 4 * 1024 * 1024) throw new InvalidDataException("Eclipse Adoptium returned too much Java runtime metadata.");
        await using var metadataStream = await metadataResponse.Content.ReadAsStreamAsync(metadataTimeout.Token);
        using var metadataBuffer = new MemoryStream();
        var metadataChunk = new byte[16 * 1024];
        while (true)
        {
            var read = await metadataStream.ReadAsync(metadataChunk.AsMemory(0, metadataChunk.Length), metadataTimeout.Token);
            if (read == 0) break;
            if (metadataBuffer.Length + read > 4 * 1024 * 1024) throw new InvalidDataException("Eclipse Adoptium returned too much Java runtime metadata.");
            metadataBuffer.Write(metadataChunk, 0, read);
        }
        metadataBuffer.Position = 0;
        using var metadata = await JsonDocument.ParseAsync(metadataBuffer, cancellationToken: metadataTimeout.Token);
        var release = metadata.RootElement.ValueKind == JsonValueKind.Array ? metadata.RootElement.EnumerateArray().FirstOrDefault() : default;
        if (release.ValueKind != JsonValueKind.Object || !release.TryGetProperty("binaries", out var binaries) || binaries.ValueKind != JsonValueKind.Array)
            throw new InvalidDataException("Eclipse Adoptium returned no compatible Java runtime.");
        var binary = binaries.EnumerateArray().FirstOrDefault(item => ReadString(item, "architecture") == "x64" && ReadString(item, "os") == "windows" && ReadString(item, "image_type") == "jre");
        if (binary.ValueKind != JsonValueKind.Object || !binary.TryGetProperty("package", out var package))
            throw new InvalidDataException("Eclipse Adoptium returned an incomplete Java runtime record.");
        var downloadUrl = ReadString(package, "link");
        var checksum = ReadString(package, "checksum");
        var publishedSize = package.TryGetProperty("size", out var sizeElement) && sizeElement.TryGetInt64(out var parsedSize) ? parsedSize : -1;
        if (!Uri.TryCreate(downloadUrl, UriKind.Absolute, out var packageUri) || packageUri.Scheme != Uri.UriSchemeHttps || packageUri.Host != "github.com" || checksum.Length != 64 || !checksum.All(Uri.IsHexDigit) || publishedSize is <= 0 or > MaximumArchiveBytes)
            throw new InvalidDataException("Eclipse Adoptium returned an invalid Java archive link, checksum, or file size.");

        var cacheDirectory = Path.Combine(appRoot, "cache");
        Directory.CreateDirectory(cacheDirectory);
        var archivePath = Path.Combine(cacheDirectory, $"temurin-{majorVersion}-{Guid.NewGuid():N}.zip");
        var staging = destination + ".tmp-" + Guid.NewGuid().ToString("N");
        var backup = destination + ".old-" + Guid.NewGuid().ToString("N");
        try
        {
            progress?.Report($"Downloading Temurin Java {majorVersion}…");
            await DownloadVerifiedAsync(packageUri, archivePath, checksum, publishedSize, progress, cancellationToken);
            progress?.Report("Safely extracting the verified Java runtime…");
            Directory.CreateDirectory(staging);
            await Task.Run(() => ExtractArchive(archivePath, staging, cancellationToken), cancellationToken);
            var stagedJava = FindJavaExecutable(staging) ?? throw new InvalidDataException("The Java archive didn't contain java.exe or javaw.exe.");
            var runtimeReport = await new JavaRuntimeInspector().InspectAsync(new InstanceProfile { JavaExecutablePath = stagedJava, RequiredJavaMajorVersion = majorVersion }, cancellationToken);
            if (!runtimeReport.IsCompatible) throw new InvalidDataException($"The downloaded runtime is Java {runtimeReport.DetectedMajorVersion}; Minecraft requires Java {majorVersion}.");

            var relativeJava = Path.GetRelativePath(staging, stagedJava);
            if (Directory.Exists(destination)) Directory.Move(destination, backup);
            try { Directory.Move(staging, destination); }
            catch
            {
                if (Directory.Exists(backup) && !Directory.Exists(destination)) Directory.Move(backup, destination);
                throw;
            }
            if (Directory.Exists(backup)) Directory.Delete(backup, recursive: true);
            var installedJava = Path.GetFullPath(Path.Combine(destination, relativeJava));
            if (!installedJava.StartsWith(destination + Path.DirectorySeparatorChar, StringComparison.OrdinalIgnoreCase)) throw new InvalidDataException("The runtime executable path escaped its installation folder.");
            progress?.Report($"Temurin Java {majorVersion} is ready.");
            return installedJava;
        }
        finally
        {
            if (Directory.Exists(staging)) Directory.Delete(staging, recursive: true);
            if (File.Exists(archivePath)) File.Delete(archivePath);
        }
    }

    private static async Task DownloadVerifiedAsync(Uri uri, string destination, string checksum, long publishedSize, IProgress<string>? progress, CancellationToken cancellationToken)
    {
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        timeout.CancelAfter(RequestTimeout);
        using var response = await Client.GetAsync(uri, HttpCompletionOption.ResponseHeadersRead, timeout.Token);
        response.EnsureSuccessStatusCode();
        var finalUri = response.RequestMessage?.RequestUri;
        if (finalUri is null || finalUri.Scheme != Uri.UriSchemeHttps || !IsTrustedReleaseHost(finalUri.Host))
            throw new InvalidDataException("The Java download redirected outside Eclipse Adoptium's GitHub release hosts.");
        if (response.Content.Headers.ContentLength is long contentLength && contentLength != publishedSize)
            throw new InvalidDataException("The Java archive size doesn't match Eclipse Adoptium's published size.");

        await using var input = await response.Content.ReadAsStreamAsync(timeout.Token);
        await using var output = new FileStream(destination, FileMode.CreateNew, FileAccess.Write, FileShare.None, 128 * 1024, useAsync: true);
        using var hash = IncrementalHash.CreateHash(HashAlgorithmName.SHA256);
        var buffer = new byte[128 * 1024];
        long received = 0;
        var lastReport = DateTimeOffset.UtcNow;
        while (true)
        {
            var read = await input.ReadAsync(buffer.AsMemory(0, buffer.Length), timeout.Token);
            if (read == 0) break;
            received += read;
            if (received > MaximumArchiveBytes || received > publishedSize) throw new InvalidDataException("The Java archive exceeded its published size.");
            hash.AppendData(buffer, 0, read);
            await output.WriteAsync(buffer.AsMemory(0, read), timeout.Token);
            if (DateTimeOffset.UtcNow - lastReport > TimeSpan.FromSeconds(1))
            {
                progress?.Report($"Downloading Temurin Java… {received / (1024 * 1024):N0} / {publishedSize / (1024 * 1024):N0} MiB");
                lastReport = DateTimeOffset.UtcNow;
            }
        }
        await output.FlushAsync(timeout.Token);
        if (received != publishedSize || !Convert.ToHexString(hash.GetHashAndReset()).Equals(checksum, StringComparison.OrdinalIgnoreCase))
            throw new InvalidDataException("The Java runtime archive failed Eclipse Adoptium's size or SHA-256 verification.");
    }

    private static void ExtractArchive(string archivePath, string destination, CancellationToken cancellationToken)
    {
        using var archive = ZipFile.OpenRead(archivePath);
        if (archive.Entries.Count > 50000) throw new InvalidDataException("The Java runtime archive contains too many files.");
        long totalBytes = 0;
        foreach (var entry in archive.Entries)
        {
            cancellationToken.ThrowIfCancellationRequested();
            var normalized = entry.FullName.Replace('\\', '/');
            if (string.IsNullOrWhiteSpace(normalized) || Path.IsPathRooted(normalized) || normalized.Split('/').Any(segment => segment is ".." or "."))
                throw new InvalidDataException("The Java runtime archive contains an unsafe path.");
            var unixMode = (entry.ExternalAttributes >> 16) & 0xF000;
            if (unixMode == 0xA000 || (entry.ExternalAttributes & (int)FileAttributes.ReparsePoint) != 0)
                throw new InvalidDataException("The Java runtime archive contains a symbolic link or reparse point.");
            var outputPath = Path.GetFullPath(Path.Combine(destination, normalized.Replace('/', Path.DirectorySeparatorChar)));
            if (!outputPath.StartsWith(destination + Path.DirectorySeparatorChar, StringComparison.OrdinalIgnoreCase))
                throw new InvalidDataException("A Java runtime archive path escaped its install folder.");
            if (normalized.EndsWith('/'))
            {
                Directory.CreateDirectory(outputPath);
                continue;
            }
            if (entry.Length > 256L * 1024 * 1024 || totalBytes + entry.Length > MaximumExtractedBytes)
                throw new InvalidDataException("The Java runtime archive exceeds Java4u's extraction limits.");
            Directory.CreateDirectory(Path.GetDirectoryName(outputPath)!);
            using var source = entry.Open();
            using var target = new FileStream(outputPath, FileMode.CreateNew, FileAccess.Write, FileShare.None);
            var buffer = new byte[64 * 1024];
            while (true)
            {
                cancellationToken.ThrowIfCancellationRequested();
                var read = source.Read(buffer, 0, buffer.Length);
                if (read == 0) break;
                if (target.Length + read > entry.Length) throw new InvalidDataException("A Java runtime file exceeded its published size.");
                target.Write(buffer, 0, read);
            }
            if (target.Length != entry.Length) throw new InvalidDataException("A Java runtime file didn't extract to its published size.");
            totalBytes += entry.Length;
        }
    }

    private static string? FindJavaExecutable(string root)
    {
        if (!Directory.Exists(root)) return null;
        return Directory.EnumerateFiles(root, "javaw.exe", SearchOption.AllDirectories).FirstOrDefault()
            ?? Directory.EnumerateFiles(root, "java.exe", SearchOption.AllDirectories).FirstOrDefault();
    }

    private static bool IsTrustedReleaseHost(string host)
        => host.Equals("github.com", StringComparison.OrdinalIgnoreCase)
           || host.Equals("release-assets.githubusercontent.com", StringComparison.OrdinalIgnoreCase)
           || host.Equals("objects.githubusercontent.com", StringComparison.OrdinalIgnoreCase)
           || host.Equals("github-releases.githubusercontent.com", StringComparison.OrdinalIgnoreCase);

    private static string ReadString(JsonElement element, string property)
        => element.ValueKind == JsonValueKind.Object && element.TryGetProperty(property, out var value) && value.ValueKind == JsonValueKind.String ? value.GetString() ?? "" : "";
}
