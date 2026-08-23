// SPDX-License-Identifier: GPL-3.0-or-later
using System.Security.Cryptography;
using System.Text;
using System.Text.Json;

namespace Bindery.Ra2.Adapter;

public sealed record RawObservation(
    string EventId,
    string CaptureId,
    ulong Sequence,
    string EventType,
    string AdapterId,
    string AdapterVersion,
    DateTimeOffset ReceivedAt,
    JsonElement Payload,
    string RawObjectHash);

public sealed record SourceRange(string CaptureId, ulong FirstSequence, ulong LastSequence, string RawObjectHash);

public sealed record NormalizedObservation(
    string DerivedEventId,
    string EventType,
    JsonElement Payload,
    IReadOnlyList<string> SourceEventIds,
    IReadOnlyList<SourceRange> SourceRanges,
    string SchemaVersion,
    string AdapterVersion,
    string NormalizerId,
    string NormalizerVersion);

public sealed class Ra2Normalizer(string version = "0.1.0")
{
    public const string NormalizerId = "bindery.ra2.normalizer";
    public const string SchemaVersion = "1.0.0";
    public string Version { get; } = version;

    public IReadOnlyList<NormalizedObservation> Normalize(IEnumerable<RawObservation> observations)
    {
        ArgumentNullException.ThrowIfNull(observations);
        List<RawObservation> ordered = observations.OrderBy(static item => item.Sequence).ThenBy(static item => item.EventId, StringComparer.Ordinal).ToList();
        List<NormalizedObservation> result = new(ordered.Count);
        foreach (RawObservation observation in ordered)
        {
            string normalizedType = observation.EventType switch
            {
                "ra2.game.started" => "game.lifecycle.started",
                "ra2.game.ended" => "game.lifecycle.ended",
                "ra2.player.joined" => "game.participant.joined",
                _ => "game.observation.unknown",
            };
            string derivedID = StableID(observation.EventId, normalizedType, Version);
            result.Add(new NormalizedObservation(
                derivedID,
                normalizedType,
                observation.Payload.Clone(),
                [observation.EventId],
                [new SourceRange(observation.CaptureId, observation.Sequence, observation.Sequence, observation.RawObjectHash)],
                SchemaVersion,
                observation.AdapterVersion,
                NormalizerId,
                Version));
        }
        return result;
    }

    private static string StableID(string sourceEventID, string normalizedType, string version)
    {
        byte[] input = Encoding.UTF8.GetBytes($"{NormalizerId}\0{version}\0{normalizedType}\0{sourceEventID}");
        return Convert.ToHexString(SHA256.HashData(input)).ToLowerInvariant();
    }
}

