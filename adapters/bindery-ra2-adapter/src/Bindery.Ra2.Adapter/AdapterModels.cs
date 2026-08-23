// SPDX-License-Identifier: GPL-3.0-or-later
namespace Bindery.Ra2.Adapter;

public enum ClientClass
{
    Player,
    Observer,
}

public enum RelayProvider
{
    BinderyNative,
    CncNetBaseline,
}

public enum LifecycleKind
{
    Ready,
    Started,
    Exited,
    Failed,
    CaptureDegraded,
}

public sealed record AdapterIdentity(string Id, string Version);

public sealed record CompatibilityHashes(string GameHash, string ModHash, string MapHash);

public sealed record AdapterConfiguration(
    Uri ServiceUri,
    string AccountToken,
    string SessionJoinCredential,
    string SessionId,
    string ClientInstanceId,
    string ClientId,
    string ClientLeaseToken,
    ClientClass ClientClass,
    AdapterIdentity Adapter,
    CompatibilityHashes Compatibility,
    RelayProvider RelayProvider,
    string? RelayHost,
    int? RelayPort,
    string? RelayCredential);

public sealed record SpawnConfiguration(
    string GameExecutable,
    string MapId,
    string PlayerName,
    string? RelayHost,
    int? RelayPort,
    string SpawnMode = "multiplayer");

public sealed record LifecycleReport(string ReportId, LifecycleKind Kind, string? Reason = null);

public sealed record DiscoveredArtifact(string Path, string Sha256, long Bytes);
