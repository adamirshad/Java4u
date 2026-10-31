//! Microsoft -> Xbox Live -> XSTS -> Minecraft services sign-in (device code flow).

use std::time::{Duration, Instant};

use reqwest::StatusCode;
use serde::Deserialize;
use serde_json::{json, Value};

use crate::error::{Error, Result};

const DEVICE_CODE_URL: &str = "https://login.microsoftonline.com/consumers/oauth2/v2.0/devicecode";
const TOKEN_URL: &str = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token";
const SCOPE: &str = "XboxLive.signin offline_access";
const XBL_URL: &str = "https://user.auth.xboxlive.com/user/authenticate";
const XSTS_URL: &str = "https://xsts.auth.xboxlive.com/xsts/authorize";
const MC_LOGIN_URL: &str = "https://api.minecraftservices.com/authentication/login_with_xbox";
const MC_PROFILE_URL: &str = "https://api.minecraftservices.com/minecraft/profile";

fn default_interval() -> u64 {
    5
}

#[derive(Debug, Clone, Deserialize)]
pub struct DeviceCode {
    pub device_code: String,
    pub user_code: String,
    pub verification_uri: String,
    pub expires_in: u64,
    #[serde(default = "default_interval")]
    pub interval: u64,
}

#[derive(Debug, Clone)]
pub struct MsTokens {
    pub access_token: String,
    pub refresh_token: String,
}

#[derive(Debug, Clone)]
pub struct McSession {
    pub access_token: String,
    pub expires_in: u64,
    pub uuid: String,
    pub username: String,
}

#[derive(Deserialize)]
struct TokenResp {
    access_token: Option<String>,
    refresh_token: Option<String>,
    error: Option<String>,
    error_description: Option<String>,
}

/// Step 1: ask Microsoft for a code the user types in at microsoft.com/link.
pub async fn request_device_code(client: &reqwest::Client, client_id: &str) -> Result<DeviceCode> {
    let resp = client
        .post(DEVICE_CODE_URL)
        .form(&[("client_id", client_id), ("scope", SCOPE)])
        .send()
        .await?;
    let status = resp.status();
    if !status.is_success() {
        let body = resp.text().await.unwrap_or_default();
        return Err(Error::Other(format!(
            "device code request failed ({status}). Check your Azure client id and that \
             'Allow public client flows' is enabled: {body}"
        )));
    }
    Ok(resp.json().await?)
}

fn tokens_from(resp: TokenResp) -> Result<MsTokens> {
    match (resp.access_token, resp.refresh_token) {
        (Some(access_token), Some(refresh_token)) => Ok(MsTokens { access_token, refresh_token }),
        _ => Err(Error::Other("Microsoft returned no tokens".into())),
    }
}

/// Step 2: poll until the user finishes signing in (or the code expires).
pub async fn poll_for_token(
    client: &reqwest::Client,
    client_id: &str,
    dc: &DeviceCode,
) -> Result<MsTokens> {
    let deadline = Instant::now() + Duration::from_secs(dc.expires_in);
    let mut interval = dc.interval.max(1);
    loop {
        tokio::time::sleep(Duration::from_secs(interval)).await;
        if Instant::now() > deadline {
            return Err(Error::Other("the login code expired; run login again".into()));
        }
        let resp: TokenResp = client
            .post(TOKEN_URL)
            .form(&[
                ("grant_type", "urn:ietf:params:oauth:grant-type:device_code"),
                ("client_id", client_id),
                ("device_code", dc.device_code.as_str()),
            ])
            .send()
            .await?
            .json()
            .await?;
        if resp.access_token.is_some() {
            return tokens_from(resp);
        }
        match resp.error.as_deref() {
            Some("authorization_pending") => continue,
            Some("slow_down") => interval += 5,
            Some("authorization_declined") | Some("access_denied") => {
                return Err(Error::Other("sign-in was declined".into()))
            }
            Some("expired_token") => {
                return Err(Error::Other("the login code expired; run login again".into()))
            }
            other => {
                return Err(Error::Other(format!(
                    "Microsoft sign-in failed: {} {}",
                    other.unwrap_or("unknown error"),
                    resp.error_description.unwrap_or_default()
                )))
            }
        }
    }
}

/// Exchange a refresh token for fresh Microsoft tokens (the refresh token rotates).
pub async fn refresh(
    client: &reqwest::Client,
    client_id: &str,
    refresh_token: &str,
) -> Result<MsTokens> {
    let resp: TokenResp = client
        .post(TOKEN_URL)
        .form(&[
            ("grant_type", "refresh_token"),
            ("client_id", client_id),
            ("refresh_token", refresh_token),
            ("scope", SCOPE),
        ])
        .send()
        .await?
        .json()
        .await?;
    if resp.access_token.is_none() {
        return Err(Error::Other(format!(
            "saved login is no longer valid, run `java4u account login` again ({})",
            resp.error_description.or(resp.error).unwrap_or_default()
        )));
    }
    tokens_from(resp)
}

async fn post_json(client: &reqwest::Client, url: &str, body: &Value) -> Result<(StatusCode, Value)> {
    let resp = client
        .post(url)
        .header("Accept", "application/json")
        .json(body)
        .send()
        .await?;
    let status = resp.status();
    let value = resp.json::<Value>().await.unwrap_or(Value::Null);
    Ok((status, value))
}

fn xsts_error(code: u64) -> &'static str {
    match code {
        2148916233 => "this Microsoft account has no Xbox profile; sign in once at minecraft.net or xbox.com to create one",
        2148916235 => "Xbox Live is not available in your country/region",
        2148916236 | 2148916237 => "this account needs adult verification on Xbox",
        2148916238 => "this is a child account; it must be added to a Microsoft Family by an adult",
        _ => "Xbox Live rejected the sign-in",
    }
}

/// Steps 3-6: Microsoft access token -> Minecraft access token + profile.
pub async fn minecraft_login(client: &reqwest::Client, ms_access_token: &str) -> Result<McSession> {
    // Xbox Live
    let (status, v) = post_json(
        client,
        XBL_URL,
        &json!({
            "Properties": {
                "AuthMethod": "RPS",
                "SiteName": "user.auth.xboxlive.com",
                "RpsTicket": format!("d={ms_access_token}")
            },
            "RelyingParty": "http://auth.xboxlive.com",
            "TokenType": "JWT"
        }),
    )
    .await?;
    if !status.is_success() {
        return Err(Error::Other(format!("Xbox Live authentication failed ({status})")));
    }
    let xbl_token = v["Token"]
        .as_str()
        .ok_or_else(|| Error::Other("Xbox Live response had no token".into()))?
        .to_string();
    let uhs = v["DisplayClaims"]["xui"][0]["uhs"]
        .as_str()
        .ok_or_else(|| Error::Other("Xbox Live response had no user hash".into()))?
        .to_string();

    // XSTS
    let (status, v) = post_json(
        client,
        XSTS_URL,
        &json!({
            "Properties": { "SandboxId": "RETAIL", "UserTokens": [xbl_token] },
            "RelyingParty": "rp://api.minecraftservices.com/",
            "TokenType": "JWT"
        }),
    )
    .await?;
    if !status.is_success() {
        let msg = v["XErr"].as_u64().map(xsts_error).unwrap_or("XSTS authorization failed");
        return Err(Error::Other(format!("{msg} ({status})")));
    }
    let xsts_token = v["Token"]
        .as_str()
        .ok_or_else(|| Error::Other("XSTS response had no token".into()))?
        .to_string();

    // Minecraft services
    let (status, v) = post_json(
        client,
        MC_LOGIN_URL,
        &json!({ "identityToken": format!("XBL3.0 x={uhs};{xsts_token}") }),
    )
    .await?;
    if status == StatusCode::FORBIDDEN {
        return Err(Error::Other(
            "Minecraft services refused the login (403). Your Azure app probably has not been \
             approved for the Minecraft API yet; see the README."
                .into(),
        ));
    }
    if !status.is_success() {
        return Err(Error::Other(format!("Minecraft login failed ({status})")));
    }
    let mc_token = v["access_token"]
        .as_str()
        .ok_or_else(|| Error::Other("Minecraft login response had no token".into()))?
        .to_string();
    let expires_in = v["expires_in"].as_u64().unwrap_or(86400);

    // Profile (404 means the account does not own Java Edition)
    let resp = client.get(MC_PROFILE_URL).bearer_auth(&mc_token).send().await?;
    let status = resp.status();
    if status == StatusCode::NOT_FOUND {
        return Err(Error::Other(
            "this Microsoft account does not own Minecraft: Java Edition".into(),
        ));
    }
    if !status.is_success() {
        return Err(Error::Other(format!("fetching the Minecraft profile failed ({status})")));
    }
    let profile: Value = resp.json().await?;
    let uuid = profile["id"]
        .as_str()
        .ok_or_else(|| Error::Other("profile had no id".into()))?
        .to_string();
    let username = profile["name"]
        .as_str()
        .ok_or_else(|| Error::Other("profile had no name".into()))?
        .to_string();

    Ok(McSession { access_token: mc_token, expires_in, uuid, username })
}
