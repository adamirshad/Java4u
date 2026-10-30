using System.IO;
using System.Text.Json;

namespace Java4u;

public sealed class LauncherSettings
{
    public string MicrosoftClientId { get; set; } = "";
}

public sealed class LauncherSettingsStore
{
    private static readonly JsonSerializerOptions JsonOptions = new() { WriteIndented = true };
    private readonly string _folder = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "Java4u");
    private string FilePath => Path.Combine(_folder, "settings.json");

    public async Task<LauncherSettings> LoadAsync(CancellationToken cancellationToken = default)
    {
        if (!File.Exists(FilePath)) return new LauncherSettings();
        await using var stream = File.OpenRead(FilePath);
        return await JsonSerializer.DeserializeAsync<LauncherSettings>(stream, cancellationToken: cancellationToken) ?? new LauncherSettings();
    }

    public async Task SaveAsync(LauncherSettings settings, CancellationToken cancellationToken = default)
    {
        Directory.CreateDirectory(_folder);
        var tempPath = FilePath + ".tmp";
        try
        {
            await using (var stream = new FileStream(tempPath, FileMode.Create, FileAccess.Write, FileShare.None, 4096, useAsync: true))
                await JsonSerializer.SerializeAsync(stream, settings, JsonOptions, cancellationToken);
            File.Move(tempPath, FilePath, overwrite: true);
        }
        finally { if (File.Exists(tempPath)) File.Delete(tempPath); }
    }
}
