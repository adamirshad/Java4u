using System.Diagnostics;
using System.IO;
using System.Text.RegularExpressions;

namespace Java4u;

public sealed class JavaRuntimeInspector
{
    private static readonly TimeSpan ProbeTimeout = TimeSpan.FromSeconds(8);

    public async Task<JavaRuntimeReport> InspectAsync(InstanceProfile profile, CancellationToken cancellationToken = default)
    {
        var javaPath = ResolveJavaExecutable(profile.JavaExecutablePath);
        if (javaPath is null)
            throw new FileNotFoundException("Java wasn't found. Install a Java runtime, add java.exe to PATH, or choose java.exe in the instance settings.");

        var probePath = ResolveConsoleJava(javaPath);
        using var process = new Process
        {
            StartInfo = new ProcessStartInfo
            {
                FileName = probePath,
                Arguments = "-version",
                UseShellExecute = false,
                CreateNoWindow = true,
                RedirectStandardError = true,
                RedirectStandardOutput = true,
            }
        };

        if (!process.Start()) throw new InvalidOperationException("Java couldn't be started to check its version.");
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(cancellationToken);
        timeout.CancelAfter(ProbeTimeout);
        var standardErrorTask = process.StandardError.ReadToEndAsync();
        var standardOutputTask = process.StandardOutput.ReadToEndAsync();
        try { await process.WaitForExitAsync(timeout.Token); }
        catch (OperationCanceledException)
        {
            try { process.Kill(entireProcessTree: true); }
            catch (InvalidOperationException) { }
            catch (System.ComponentModel.Win32Exception) { }
            if (cancellationToken.IsCancellationRequested) throw;
            throw new TimeoutException("Java didn't respond to its version check within eight seconds.");
        }

        var output = (await standardErrorTask) + "\n" + (await standardOutputTask);
        if (process.ExitCode != 0) throw new InvalidOperationException("The selected Java executable returned an error while checking its version.");
        var match = Regex.Match(output, """(?:version "|openjdk version ")(?<version>1\.\d+|\d+)""", RegexOptions.IgnoreCase | RegexOptions.CultureInvariant);
        if (!match.Success || !int.TryParse(match.Groups["version"].Value.Split('.').Last(), out var major))
            throw new InvalidDataException("Java started, but Java4u couldn't read its major version.");
        if (match.Groups["version"].Value.StartsWith("1.", StringComparison.Ordinal)) major = int.Parse(match.Groups["version"].Value.Split('.')[1]);

        var required = profile.RequiredJavaMajorVersion;
        var compatible = required == 0 || required == major;
        return new JavaRuntimeReport(javaPath, major, required, compatible);
    }

    private static string? ResolveJavaExecutable(string configuredPath)
    {
        if (!string.IsNullOrWhiteSpace(configuredPath))
            return File.Exists(configuredPath) ? Path.GetFullPath(configuredPath) : null;

        var javaHome = Environment.GetEnvironmentVariable("JAVA_HOME");
        if (!string.IsNullOrWhiteSpace(javaHome))
        {
            var candidate = Path.Combine(javaHome, "bin", "java.exe");
            if (File.Exists(candidate)) return Path.GetFullPath(candidate);
        }

        foreach (var directory in (Environment.GetEnvironmentVariable("PATH") ?? "").Split(Path.PathSeparator, StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries))
        {
            try
            {
                var candidate = Path.Combine(directory.Trim('"'), "java.exe");
                if (File.Exists(candidate)) return Path.GetFullPath(candidate);
            }
            catch (ArgumentException) { }
            catch (PathTooLongException) { }
        }
        return null;
    }

    private static string ResolveConsoleJava(string javaPath)
    {
        if (!Path.GetFileName(javaPath).Equals("javaw.exe", StringComparison.OrdinalIgnoreCase)) return javaPath;
        var consolePath = Path.Combine(Path.GetDirectoryName(javaPath) ?? "", "java.exe");
        return File.Exists(consolePath) ? consolePath : javaPath;
    }
}

public sealed record JavaRuntimeReport(string ExecutablePath, int DetectedMajorVersion, int RequiredMajorVersion, bool IsCompatible)
{
    public string Summary => RequiredMajorVersion == 0
        ? $"Java {DetectedMajorVersion} detected. Install this instance's game files to check the version Minecraft requires."
        : IsCompatible
            ? $"Java {DetectedMajorVersion} matches Minecraft's required Java {RequiredMajorVersion}."
            : $"Java {DetectedMajorVersion} was found, but Minecraft requires Java {RequiredMajorVersion}. Choose a matching runtime in this instance's settings.";
}
