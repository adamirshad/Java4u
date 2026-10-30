using System.Text.Json.Serialization;

namespace Java4u;

public sealed class InstanceProfile
{
    public Guid Id { get; set; } = Guid.NewGuid();
    public string Name { get; set; } = "";
    public string MinecraftVersion { get; set; } = "1.21.4";
    public string Loader { get; set; } = "Chunky";
    public string JavaExecutablePath { get; set; } = "";
    public int RequiredJavaMajorVersion { get; set; }
    public string InstalledMinecraftVersion { get; set; } = "";
    public DateTime? InstalledUtc { get; set; }
    public string InstanceDirectory { get; set; } = "";
    public int MemoryLimitMb { get; set; } = 4096;
    [JsonIgnore]
    public int MemoryLimitGb => Math.Max(1, (MemoryLimitMb + 1023) / 1024);
    [JsonIgnore]
    public bool IsInstalled => InstalledMinecraftVersion == MinecraftVersion;
    public DateTime CreatedUtc { get; set; } = DateTime.UtcNow;
}
