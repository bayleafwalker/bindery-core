// SPDX-License-Identifier: GPL-3.0-or-later
using System.Net.Http.Headers;
using System.Net.Http.Json;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace Bindery.Ra2.Adapter;

public sealed class BinderyAdapterClient(HttpClient httpClient)
{
    private readonly JsonSerializerOptions json = new(JsonSerializerDefaults.Web);

    public async Task ReportAsync(AdapterConfiguration configuration, LifecycleReport report, CancellationToken cancellationToken)
    {
        using HttpRequestMessage request = new(HttpMethod.Post, $"/v1/enrollments/{configuration.ClientId}/reports")
        {
            Content = JsonContent.Create(report, options: json),
        };
        request.Headers.Authorization = new AuthenticationHeaderValue("Bearer", configuration.ClientLeaseToken);
        request.Headers.Add("Idempotency-Key", report.ReportId);
        using HttpResponseMessage response = await httpClient.SendAsync(request, cancellationToken).ConfigureAwait(false);
        response.EnsureSuccessStatusCode();
    }

    public async Task<IdentityCredentials> CreateIdentityAsync(string handle, string? displayName, string idempotencyKey, CancellationToken cancellationToken)
    {
        using HttpRequestMessage request = new(HttpMethod.Post, "/v1/identities")
        {
            Content = JsonContent.Create(new { handle, display_name = displayName }, options: json),
        };
        request.Headers.Add("Idempotency-Key", idempotencyKey);
        using HttpResponseMessage response = await httpClient.SendAsync(request, cancellationToken).ConfigureAwait(false);
        response.EnsureSuccessStatusCode();
        IdentityCreateDto dto = await response.Content.ReadFromJsonAsync<IdentityCreateDto>(json, cancellationToken).ConfigureAwait(false) ?? throw new InvalidOperationException("identity response was empty");
        return new IdentityCredentials(dto.PublicIdentity.AccountId, dto.AccountToken);
    }

    public async Task<SessionCredentials> CreateSessionAsync(string accountToken, object sessionRequest, string idempotencyKey, CancellationToken cancellationToken)
    {
        using HttpRequestMessage request = new(HttpMethod.Post, "/v1/sessions")
        {
            Content = JsonContent.Create(sessionRequest, options: json),
        };
        request.Headers.Authorization = new AuthenticationHeaderValue("Bearer", accountToken);
        request.Headers.Add("Idempotency-Key", idempotencyKey);
        using HttpResponseMessage response = await httpClient.SendAsync(request, cancellationToken).ConfigureAwait(false);
        response.EnsureSuccessStatusCode();
        SessionCreateDto dto = await response.Content.ReadFromJsonAsync<SessionCreateDto>(json, cancellationToken).ConfigureAwait(false) ?? throw new InvalidOperationException("session response was empty");
        return new SessionCredentials(dto.PublicSession.SessionId, dto.SessionJoinCredential);
    }

    public async Task<EnrollmentCredentials> EnrollAsync(AdapterConfiguration configuration, string idempotencyKey, CancellationToken cancellationToken)
    {
        object requestBody = new
        {
            client_instance_id = configuration.ClientInstanceId,
            client_class = configuration.ClientClass == ClientClass.Observer ? "observer" : "player",
            adapter = new { id = configuration.Adapter.Id, version = configuration.Adapter.Version },
            compatibility = new { game_hash = configuration.Compatibility.GameHash, mod_hash = configuration.Compatibility.ModHash, map_hash = configuration.Compatibility.MapHash },
        };
        using HttpRequestMessage request = new(HttpMethod.Post, $"/v1/sessions/{configuration.SessionId}/enrollments")
        {
            Content = JsonContent.Create(requestBody, options: json),
        };
        request.Headers.Authorization = new AuthenticationHeaderValue("Bearer", configuration.AccountToken);
        request.Headers.Add("X-Session-Join-Credential", configuration.SessionJoinCredential);
        request.Headers.Add("Idempotency-Key", idempotencyKey);
        using HttpResponseMessage response = await httpClient.SendAsync(request, cancellationToken).ConfigureAwait(false);
        response.EnsureSuccessStatusCode();
        EnrollmentCreateDto dto = await response.Content.ReadFromJsonAsync<EnrollmentCreateDto>(json, cancellationToken).ConfigureAwait(false) ?? throw new InvalidOperationException("enrollment response was empty");
        return new EnrollmentCredentials(dto.PublicEnrollment.ClientId, dto.ClientLeaseToken, dto.TransportCredential);
    }
}

internal sealed record PublicIdentityDto([property: JsonPropertyName("account_id")] string AccountId);
public sealed record IdentityCredentials(string AccountId, string AccountToken);
public sealed record SessionCredentials(string SessionId, string SessionJoinCredential);
public sealed record EnrollmentCredentials(string ClientId, string ClientLeaseToken, string TransportCredential);

internal sealed record IdentityCreateDto([property: JsonPropertyName("public_identity")] PublicIdentityDto PublicIdentity, [property: JsonPropertyName("account_token")] string AccountToken);
internal sealed record SessionCreateDto([property: JsonPropertyName("public_session")] SessionPublicDto PublicSession, [property: JsonPropertyName("session_join_credential")] string SessionJoinCredential);
internal sealed record SessionPublicDto([property: JsonPropertyName("session_id")] string SessionId);
internal sealed record EnrollmentCreateDto([property: JsonPropertyName("public_enrollment")] EnrollmentPublicDto PublicEnrollment, [property: JsonPropertyName("client_lease_token")] string ClientLeaseToken, [property: JsonPropertyName("transport_credential")] string TransportCredential);
internal sealed record EnrollmentPublicDto([property: JsonPropertyName("client_id")] string ClientId);
