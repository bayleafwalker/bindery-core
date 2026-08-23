// SPDX-License-Identifier: GPL-3.0-or-later
using System.Security.Cryptography;
using System.Text;

namespace Bindery.Ra2.Adapter;

public static class Hashing
{
    public static string Sha256File(string path)
    {
        ArgumentException.ThrowIfNullOrWhiteSpace(path);
        using FileStream stream = File.OpenRead(path);
        return "sha256:" + Convert.ToHexString(SHA256.HashData(stream)).ToLowerInvariant();
    }

    public static string Sha256Directory(string directory)
    {
        ArgumentException.ThrowIfNullOrWhiteSpace(directory);
        if (!Directory.Exists(directory)) throw new DirectoryNotFoundException(directory);
        StringBuilder manifest = new();
        foreach (string path in Directory.EnumerateFiles(directory, "*", SearchOption.AllDirectories).OrderBy(static p => p, StringComparer.OrdinalIgnoreCase))
        {
            string relative = Path.GetRelativePath(directory, path).Replace('\\', '/');
            manifest.Append(relative).Append('\0').Append(Sha256File(path)).Append('\n');
        }
        return "sha256:" + Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(manifest.ToString()))).ToLowerInvariant();
    }
}

