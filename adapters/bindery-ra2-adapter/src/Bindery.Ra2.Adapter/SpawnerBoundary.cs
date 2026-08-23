// SPDX-License-Identifier: GPL-3.0-or-later
using System.Diagnostics;

namespace Bindery.Ra2.Adapter;

public interface ISpawnerBoundary
{
    Task<Process> StartAsync(string executable, string workingDirectory, string spawnIniPath, CancellationToken cancellationToken);
}

public sealed class WindowsSpawnerBoundary : ISpawnerBoundary
{
    public Task<Process> StartAsync(string executable, string workingDirectory, string spawnIniPath, CancellationToken cancellationToken)
    {
        if (!OperatingSystem.IsWindows()) throw new PlatformNotSupportedException("RA2/YR process integration is Windows-only");
        ArgumentException.ThrowIfNullOrWhiteSpace(executable);
        ArgumentException.ThrowIfNullOrWhiteSpace(spawnIniPath);
        ProcessStartInfo startInfo = new()
        {
            FileName = executable,
            WorkingDirectory = workingDirectory,
            UseShellExecute = false,
            CreateNoWindow = false,
            Arguments = "-SPAWN",
        };
        startInfo.Environment["BINDERY_SPAWN_INI"] = spawnIniPath;
        Process process = Process.Start(startInfo) ?? throw new InvalidOperationException("game process did not start");
        return Task.FromResult(process);
    }
}
