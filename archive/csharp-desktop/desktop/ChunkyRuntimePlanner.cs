using System.IO;
using System.Text;

namespace Java4u;

public sealed record ChunkyRuntimePlan(IReadOnlyList<string> ModIds, IReadOnlyList<ChunkyEntrypoint> Entrypoints)
{
    public string EncodedEntrypoints => string.Join(',', Entrypoints.Select(entry =>
        Convert.ToBase64String(Encoding.UTF8.GetBytes($"{entry.ModId}\t{entry.Kind}\t{entry.ClassName}"))
            .TrimEnd('=').Replace('+', '-').Replace('/', '_')));
}

public sealed class ChunkyRuntimePlanner
{
    public async Task<ChunkyRuntimePlan> CreatePlanAsync(InstanceProfile profile, CancellationToken cancellationToken = default)
    {
        var report = await new ChunkyModInspector().InspectAsync(profile, cancellationToken);
        if (report.Issues.Count > 0)
        {
            var details = string.Join(Environment.NewLine, report.Issues.Take(8).Select(issue => $"{issue.FileName}: {issue.Message}"));
            if (report.Issues.Count > 8) details += Environment.NewLine + $"…and {report.Issues.Count - 8} more issue(s).";
            throw new InvalidOperationException("Chunky alpha can't launch this mod set yet:" + Environment.NewLine + details);
        }
        if (report.Mods.Count > 256) throw new InvalidDataException("Chunky alpha supports at most 256 installed mods per instance.");

        var mods = report.Mods.ToDictionary(mod => mod.Id, StringComparer.Ordinal);
        var ordered = new List<ChunkyModRecord>(mods.Count);
        var state = new Dictionary<string, byte>(StringComparer.Ordinal);
        foreach (var mod in report.Mods.OrderBy(mod => mod.Id, StringComparer.Ordinal)) Visit(mod);
        var entrypoints = ordered.SelectMany(mod => mod.Entrypoints).ToArray();
        if (entrypoints.Length > 64) throw new InvalidDataException("Chunky alpha supports at most 64 mod entrypoints per instance.");
        var plan = new ChunkyRuntimePlan(ordered.Select(mod => mod.Id).ToArray(), entrypoints);
        if (plan.ModIds.Sum(id => id.Length + 1) > 4096 || plan.EncodedEntrypoints.Length > 4096)
            throw new InvalidDataException("The Chunky mod launch plan is too large for a safe Windows process command line.");
        return plan;

        void Visit(ChunkyModRecord mod)
        {
            cancellationToken.ThrowIfCancellationRequested();
            if (state.TryGetValue(mod.Id, out var existing))
            {
                if (existing == 1) throw new InvalidDataException($"Chunky found a dependency cycle involving mod '{mod.Id}'.");
                if (existing == 2) return;
            }
            state[mod.Id] = 1;
            foreach (var dependencyId in mod.RequiredMods.OrderBy(id => id, StringComparer.Ordinal))
                if (mods.TryGetValue(dependencyId, out var dependency)) Visit(dependency);
            state[mod.Id] = 2;
            ordered.Add(mod);
        }
    }
}
