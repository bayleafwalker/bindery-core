// SPDX-License-Identifier: GPL-3.0-or-later
namespace Bindery.Ra2.Adapter;

public static class ArtifactDiscovery
{
    private static readonly HashSet<string> allowedExtensions = new(StringComparer.OrdinalIgnoreCase) { ".log", ".txt", ".json", ".yaml", ".yml", ".ini", ".zip", ".dmp" };

    public static IReadOnlyList<DiscoveredArtifact> Discover(string directory)
    {
        if (!Directory.Exists(directory)) return [];
        return Directory.EnumerateFiles(directory, "*", SearchOption.AllDirectories)
            .Where(path => allowedExtensions.Contains(Path.GetExtension(path)))
            .OrderBy(path => path, StringComparer.OrdinalIgnoreCase)
            .Select(path => new DiscoveredArtifact(path, Hashing.Sha256File(path), new FileInfo(path).Length))
            .ToArray();
    }
}

