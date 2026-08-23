// SPDX-License-Identifier: GPL-3.0-or-later
using System.Diagnostics;

namespace Bindery.Ra2.Adapter;

public sealed class ProcessLifecycle(ISpawnerBoundary spawner)
{
    public async Task<int> RunAsync(SpawnConfiguration configuration, string workingDirectory, string spawnIniPath, Func<LifecycleReport, Task> report, CancellationToken cancellationToken)
    {
        ArgumentNullException.ThrowIfNull(report);
        await report(new LifecycleReport(Guid.NewGuid().ToString(), LifecycleKind.Ready)).ConfigureAwait(false);
        Process process;
        try
        {
            process = await spawner.StartAsync(configuration.GameExecutable, workingDirectory, spawnIniPath, cancellationToken).ConfigureAwait(false);
            await report(new LifecycleReport(Guid.NewGuid().ToString(), LifecycleKind.Started)).ConfigureAwait(false);
        }
        catch (Exception exception) when (exception is InvalidOperationException or PlatformNotSupportedException or IOException)
        {
            await report(new LifecycleReport(Guid.NewGuid().ToString(), LifecycleKind.Failed, exception.GetType().Name)).ConfigureAwait(false);
            throw;
        }

        await process.WaitForExitAsync(cancellationToken).ConfigureAwait(false);
        LifecycleKind kind = process.ExitCode == 0 ? LifecycleKind.Exited : LifecycleKind.Failed;
        await report(new LifecycleReport(Guid.NewGuid().ToString(), kind, process.ExitCode.ToString(System.Globalization.CultureInfo.InvariantCulture))).ConfigureAwait(false);
        return process.ExitCode;
    }
}
