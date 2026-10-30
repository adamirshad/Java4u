using System.IO;
using System.Net;
using System.Net.Http;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace Java4u;

public sealed record DeviceSignInRequest(string DeviceCode, string UserCode, string VerificationUri, int ExpiresInSeconds, int PollIntervalSeconds);
public sealed record AuthProgress(string Message);
public sealed class MinecraftReauthenticationRequiredException : InvalidOperationException
{
    public MinecraftReauthenticationRequiredException() : base("Microsoft sign-in expired or was revoked. Sign in again with your Microsoft account.") { }
}

public sealed class MinecraftAuthService
{
    private static readonly HttpClient Client = new() { Timeout = TimeSpan.FromSeconds(30) };
    private const string Scope = "XboxLive.signin offline_access";
    private const string DeviceCodeEndpoint = "https://login.microsoftonline.com/consumers/oauth2/v2.0/devicecode";
    private const string TokenEndpoint = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token";

    public async Task<DeviceSignInRequest> BeginSignInAsync(string clientId, CancellationToken cancellationToken = default)
    {
        ValidateClientId(clientId);
        using var response = await Client.PostAsync(DeviceCodeEndpoint, new FormUrlEncodedContent(new Dictionary<string, string>
        {
            ["client_id"] = clientId,
            ["scope"] = Scope,
        }), cancellationToken);
        var body = await response.Content.ReadAsStringAsync(cancellationToken);
        EnsureSuccess(response.StatusCode, body, "Microsoft sign-in couldn't start.");
        var result = JsonSerializer.Deserialize<DeviceCodeResponse>(body) ?? throw new InvalidDataException("Microsoft returned an empty sign-in response.");
        if (string.IsNullOrWhiteSpace(result.DeviceCode) || string.IsNullOrWhiteSpace(result.UserCode) || !Uri.TryCreate(result.VerificationUri, UriKind.Absolute, out var uri) || uri.Scheme != Uri.UriSchemeHttps)
            throw new InvalidDataException("Microsoft returned incomplete sign-in instructions.");
        return new DeviceSignInRequest(result.DeviceCode, result.UserCode, result.VerificationUri, result.ExpiresIn, Math.Max(1, result.Interval));
    }

    public async Task<MinecraftAuthSession> CompleteSignInAsync(string clientId, DeviceSignInRequest request, IProgress<AuthProgress>? progress = null, CancellationToken cancellationToken = default)
    {
        ValidateClientId(clientId);
        var deadline = DateTimeOffset.UtcNow.AddSeconds(request.ExpiresInSeconds);
        var interval = Math.Max(1, request.PollIntervalSeconds);
        MicrosoftTokenResponse? microsoft = null;
        while (DateTimeOffset.UtcNow < deadline)
        {
            await Task.Delay(TimeSpan.FromSeconds(interval), cancellationToken);
            using var response = await Client.PostAsync(TokenEndpoint, new FormUrlEncodedContent(new Dictionary<string, string>
            {
                ["grant_type"] = "urn:ietf:params:oauth:grant-type:device_code",
                ["client_id"] = clientId,
                ["device_code"] = request.DeviceCode,
            }), cancellationToken);
            var body = await response.Content.ReadAsStringAsync(cancellationToken);
            if (response.IsSuccessStatusCode)
            {
                microsoft = JsonSerializer.Deserialize<MicrosoftTokenResponse>(body) ?? throw new InvalidDataException("Microsoft returned an empty token response.");
                break;
            }
            var error = TryGetOAuthError(body);
            if (error == "authorization_pending")
            {
                progress?.Report(new AuthProgress("Waiting for you to finish signing in with Microsoft…"));
                continue;
            }
            if (error == "slow_down")
            {
                interval += 5;
                continue;
            }
            if (error is "authorization_declined" or "bad_verification_code" or "expired_token")
                throw new InvalidOperationException(error == "authorization_declined" ? "Microsoft sign-in was declined." : "The Microsoft sign-in code expired or was rejected. Start sign-in again.");
            EnsureSuccess(response.StatusCode, body, "Microsoft sign-in failed.");
        }

        if (microsoft is null) throw new TimeoutException("The Microsoft sign-in code expired. Start sign-in again.");
        if (string.IsNullOrWhiteSpace(microsoft.RefreshToken)) throw new InvalidDataException("Microsoft didn't return a refresh token. Check that offline_access is enabled for Java4u's public-client app registration.");
        progress?.Report(new AuthProgress("Checking the Microsoft and Xbox account…"));
        return await ExchangeForMinecraftAsync(clientId, microsoft.AccessToken, microsoft.RefreshToken, microsoft.ExpiresIn, cancellationToken);
    }

    public async Task<MinecraftAuthSession> RefreshAndValidateAsync(string clientId, string refreshToken, CancellationToken cancellationToken = default)
    {
        ValidateClientId(clientId);
        using var response = await Client.PostAsync(TokenEndpoint, new FormUrlEncodedContent(new Dictionary<string, string>
        {
            ["client_id"] = clientId,
            ["grant_type"] = "refresh_token",
            ["refresh_token"] = refreshToken,
            ["scope"] = Scope,
        }), cancellationToken);
        var body = await response.Content.ReadAsStringAsync(cancellationToken);
        if (!response.IsSuccessStatusCode && TryGetOAuthError(body) == "invalid_grant")
            throw new MinecraftReauthenticationRequiredException();
        EnsureSuccess(response.StatusCode, body, "Microsoft sign-in expired. Sign in again.");
        var microsoft = JsonSerializer.Deserialize<MicrosoftTokenResponse>(body) ?? throw new InvalidDataException("Microsoft returned an empty refresh response.");
        var newRefresh = string.IsNullOrWhiteSpace(microsoft.RefreshToken) ? refreshToken : microsoft.RefreshToken;
        return await ExchangeForMinecraftAsync(clientId, microsoft.AccessToken, newRefresh, microsoft.ExpiresIn, cancellationToken);
    }

    private static async Task<MinecraftAuthSession> ExchangeForMinecraftAsync(string clientId, string microsoftAccessToken, string refreshToken, int microsoftExpiresIn, CancellationToken cancellationToken)
    {
        using var xblRequest = new HttpRequestMessage(HttpMethod.Post, "https://user.auth.xboxlive.com/user/authenticate")
        {
            Content = JsonContent(new
            {
                Properties = new { AuthMethod = "RPS", SiteName = "user.auth.xboxlive.com", RpsTicket = "d=" + microsoftAccessToken },
                RelyingParty = "http://auth.xboxlive.com",
                TokenType = "JWT",
            })
        };
        xblRequest.Headers.TryAddWithoutValidation("x-xbl-contract-version", "1");
        var xbl = await SendJsonAsync<XboxTokenResponse>(xblRequest, "Xbox sign-in failed.", cancellationToken);
        var userHash = xbl.DisplayClaims?.Xui?.FirstOrDefault()?.UserHash;
        if (string.IsNullOrWhiteSpace(xbl.Token) || string.IsNullOrWhiteSpace(userHash)) throw new InvalidDataException("Xbox didn't return the account identity required by Minecraft.");

        using var xstsRequest = new HttpRequestMessage(HttpMethod.Post, "https://xsts.auth.xboxlive.com/xsts/authorize")
        {
            Content = JsonContent(new
            {
                Properties = new { SandboxId = "RETAIL", UserTokens = new[] { xbl.Token } },
                RelyingParty = "rp://api.minecraftservices.com/",
                TokenType = "JWT",
            })
        };
        xstsRequest.Headers.TryAddWithoutValidation("x-xbl-contract-version", "1");
        var xsts = await SendJsonAsync<XboxTokenResponse>(xstsRequest, "Xbox couldn't authorize this account for Minecraft.", cancellationToken);
        if (string.IsNullOrWhiteSpace(xsts.Token)) throw new InvalidDataException("Xbox didn't return an authorization token for Minecraft.");

        var xstsUserHash = xsts.DisplayClaims?.Xui?.FirstOrDefault()?.UserHash ?? userHash;
        using var minecraftRequest = new HttpRequestMessage(HttpMethod.Post, "https://api.minecraftservices.com/authentication/login_with_xbox")
        {
            Content = JsonContent(new { identityToken = $"XBL3.0 x={xstsUserHash};{xsts.Token}" })
        };
        var minecraft = await SendJsonAsync<MinecraftTokenResponse>(minecraftRequest, "Minecraft account sign-in failed.", cancellationToken);
        if (string.IsNullOrWhiteSpace(minecraft.AccessToken)) throw new InvalidDataException("Minecraft didn't return a game access token.");

        using var entitlementRequest = new HttpRequestMessage(HttpMethod.Get, "https://api.minecraftservices.com/entitlements/mcstore");
        entitlementRequest.Headers.Authorization = new System.Net.Http.Headers.AuthenticationHeaderValue("Bearer", minecraft.AccessToken);
        var entitlements = await SendJsonAsync<EntitlementsResponse>(entitlementRequest, "Minecraft couldn't check the account's game license.", cancellationToken);
        var ownsJava = entitlements.Items?.Any(item => item.Name is "game_minecraft" or "product_minecraft") == true;
        if (!ownsJava) throw new InvalidOperationException("This Microsoft account has no Minecraft: Java Edition entitlement. Sign in with the account that owns the game.");

        using var profileRequest = new HttpRequestMessage(HttpMethod.Get, "https://api.minecraftservices.com/minecraft/profile");
        profileRequest.Headers.Authorization = new System.Net.Http.Headers.AuthenticationHeaderValue("Bearer", minecraft.AccessToken);
        var profile = await SendJsonAsync<MinecraftProfileResponse>(profileRequest, "The account owns Minecraft but doesn't have a Java profile set up yet.", cancellationToken);
        if (string.IsNullOrWhiteSpace(profile.Name) || string.IsNullOrWhiteSpace(profile.Id)) throw new InvalidDataException("Minecraft returned an incomplete Java profile.");

        var expirySeconds = Math.Clamp(minecraft.ExpiresIn, 60, 86400);
        var xuid = xsts.DisplayClaims?.Xui?.FirstOrDefault()?.Xuid ?? xbl.DisplayClaims?.Xui?.FirstOrDefault()?.Xuid ?? "";
        return new MinecraftAuthSession(profile.Name, profile.Id, xuid, minecraft.AccessToken, refreshToken, DateTimeOffset.UtcNow.AddSeconds(expirySeconds));
    }

    private static async Task<T> SendJsonAsync<T>(HttpRequestMessage request, string errorPrefix, CancellationToken cancellationToken)
    {
        using var response = await Client.SendAsync(request, cancellationToken);
        var body = await response.Content.ReadAsStringAsync(cancellationToken);
        EnsureSuccess(response.StatusCode, body, errorPrefix);
        return JsonSerializer.Deserialize<T>(body) ?? throw new InvalidDataException(errorPrefix);
    }

    private static StringContent JsonContent<T>(T value) => new(JsonSerializer.Serialize(value), System.Text.Encoding.UTF8, "application/json");

    private static void EnsureSuccess(HttpStatusCode status, string responseBody, string prefix)
    {
        if ((int)status is >= 200 and < 300) return;
        var detail = TryGetErrorDescription(responseBody);
        if (status == HttpStatusCode.Forbidden && responseBody.Contains("Invalid app registration", StringComparison.OrdinalIgnoreCase))
            throw new InvalidOperationException("Microsoft sign-in worked, but Mojang hasn't approved Java4u's app registration for Minecraft Java services yet. The Minecraft app registration must be submitted for review.");
        throw new HttpRequestException(string.IsNullOrWhiteSpace(detail) ? $"{prefix} (HTTP {(int)status})" : $"{prefix} {detail}", null, status);
    }

    private static string? TryGetOAuthError(string body)
    {
        try { using var json = JsonDocument.Parse(body); return json.RootElement.TryGetProperty("error", out var value) ? value.GetString() : null; }
        catch (JsonException) { return null; }
    }

    private static string? TryGetErrorDescription(string body)
    {
        try
        {
            using var json = JsonDocument.Parse(body);
            if (json.RootElement.TryGetProperty("error_description", out var description)) return description.GetString();
            if (json.RootElement.TryGetProperty("errorMessage", out var message)) return message.GetString();
            if (json.RootElement.TryGetProperty("XErr", out var xerr)) return $"Xbox error {xerr}.";
            return null;
        }
        catch (JsonException) { return null; }
    }

    private static void ValidateClientId(string clientId)
    {
        if (!Guid.TryParse(clientId, out _)) throw new InvalidOperationException("Add Java4u's Microsoft app registration client ID in Settings first.");
    }

    private sealed class DeviceCodeResponse
    {
        [JsonPropertyName("device_code")] public string DeviceCode { get; init; } = "";
        [JsonPropertyName("user_code")] public string UserCode { get; init; } = "";
        [JsonPropertyName("verification_uri")] public string VerificationUri { get; init; } = "";
        [JsonPropertyName("expires_in")] public int ExpiresIn { get; init; }
        [JsonPropertyName("interval")] public int Interval { get; init; } = 5;
    }

    private sealed class MicrosoftTokenResponse
    {
        [JsonPropertyName("access_token")] public string AccessToken { get; init; } = "";
        [JsonPropertyName("refresh_token")] public string RefreshToken { get; init; } = "";
        [JsonPropertyName("expires_in")] public int ExpiresIn { get; init; }
    }

    private sealed class XboxTokenResponse
    {
        [JsonPropertyName("Token")] public string Token { get; init; } = "";
        [JsonPropertyName("DisplayClaims")] public XboxDisplayClaims? DisplayClaims { get; init; }
    }

    private sealed class XboxDisplayClaims
    {
        [JsonPropertyName("xui")] public List<XboxUserClaim>? Xui { get; init; }
    }

    private sealed class XboxUserClaim
    {
        [JsonPropertyName("uhs")] public string UserHash { get; init; } = "";
        [JsonPropertyName("xid")] public string Xuid { get; init; } = "";
    }

    private sealed class MinecraftTokenResponse
    {
        [JsonPropertyName("access_token")] public string AccessToken { get; init; } = "";
        [JsonPropertyName("expires_in")] public int ExpiresIn { get; init; }
    }

    private sealed class EntitlementsResponse
    {
        [JsonPropertyName("items")] public List<EntitlementItem>? Items { get; init; }
    }

    private sealed class EntitlementItem
    {
        [JsonPropertyName("name")] public string Name { get; init; } = "";
    }

    private sealed class MinecraftProfileResponse
    {
        [JsonPropertyName("id")] public string Id { get; init; } = "";
        [JsonPropertyName("name")] public string Name { get; init; } = "";
    }
}
