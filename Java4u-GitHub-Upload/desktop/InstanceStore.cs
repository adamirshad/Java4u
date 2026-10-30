using System.IO;
using System.Text.Json;

namespace Java4u;

public sealed class InstanceStore
{
    private static readonly JsonSerializerOptions JsonOptions = new() { WriteIndented = true };
    private readonly string _directory;
    private readonly string _instancesDirectory;
    private readonly string _filePath;

    public InstanceStore()
    {
        _directory = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Java4u");
        _instancesDirectory = Path.Combine(_directory, "instances");
        _filePath = Path.Combine(_directory, "instances.json");
    }

    public string PrepareInstanceDirectory(InstanceProfile profile)
    {
        var root = Path.GetFullPath(_instancesDirectory);
        var path = Path.GetFullPath(Path.Combine(root, profile.Id.ToString("N")));
        if (!path.StartsWith(root + Path.DirectorySeparatorChar, StringComparison.OrdinalIgnoreCase))
            throw new InvalidOperationException("The instance path is outside the Java4u data directory.");
        Directory.CreateDirectory(path);
        var gameDirectory = Path.Combine(path, ".minecraft");
        foreach (var folder in new[] { "mods", "saves", "config", "resourcepacks", "shaderpacks", "screenshots", "logs", "crash-reports", "versions", "libraries", "assets" })
            Directory.CreateDirectory(Path.Combine(gameDirectory, folder));
        profile.InstanceDirectory = path;
        return path;
    }

    public async Task<List<InstanceProfile>> LoadAsync()
    {
        if (!File.Exists(_filePath)) return [];
        try
        {
            await using var stream = File.OpenRead(_filePath);
            return await JsonSerializer.DeserializeAsync<List<InstanceProfile>>(stream) ?? [];
        }
        catch (JsonException)
        {
            var backupPath = _filePath + ".corrupt-" + DateTime.UtcNow.ToString("yyyyMMdd-HHmmss");
            File.Move(_filePath, backupPath, overwrite: false);
            return [];
        }
    }

    public async Task SaveAsync(IReadOnlyCollection<InstanceProfile> instances)
    {
        Directory.CreateDirectory(_directory);
        var tempPath = _filePath + ".tmp";
        try
        {
            await using (var stream = new FileStream(tempPath, FileMode.Create, FileAccess.Write, FileShare.None, 4096, useAsync: true))
            {
                await JsonSerializer.SerializeAsync(stream, instances, JsonOptions);
                await stream.FlushAsync();
            }

            if (File.Exists(_filePath))
            {
                try { File.Replace(tempPath, _filePath, null); }
                catch (PlatformNotSupportedException) { File.Move(tempPath, _filePath, overwrite: true); }
            }
            else File.Move(tempPath, _filePath);
        }
        finally { if (File.Exists(tempPath)) File.Delete(tempPath); }
    }
}
