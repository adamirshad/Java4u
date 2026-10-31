//! Persistent Microsoft accounts and the login/refresh orchestration.

use std::io::Write;
use std::path::Path;
use std::time::{SystemTime, UNIX_EPOCH};

use serde::{Deserialize, Serialize};

use crate::auth::Account;
use crate::error::{Error, Result};
use crate::microsoft::{self, DeviceCode, McSession};
use crate::paths::Dirs;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StoredAccount {
    pub username: String,
    pub uuid: String,
    pub refresh_token: String,
    pub mc_access_token: String,
    /// Unix seconds.
    pub mc_expires_at: u64,
}

#[derive(Debug, Default, Serialize, Deserialize)]
pub struct AccountStore {
    #[serde(default)]
    pub client_id: Option<String>,
    #[serde(default)]
    pub accounts: Vec<StoredAccount>,
}

fn now() -> u64 {
    SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_secs()).unwrap_or(0)
}

/// Write a file readable only by the current user (on Unix), via temp file + rename.
fn write_private(path: &Path, data: &[u8]) -> Result<()> {
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent)?;
    }
    let tmp = path.with_extension("json.tmp");
    {
        let mut opts = std::fs::OpenOptions::new();
        opts.write(true).create(true).truncate(true);
        #[cfg(unix)]
        {
            use std::os::unix::fs::OpenOptionsExt;
            opts.mode(0o600);
        }
        let mut f = opts.open(&tmp)?;
        f.write_all(data)?;
    }
    std::fs::rename(&tmp, path)?;
    Ok(())
}

impl AccountStore {
    pub fn load(dirs: &Dirs) -> Result<Self> {
        match std::fs::read_to_string(dirs.accounts_file()) {
            Ok(text) => Ok(serde_json::from_str(&text)?),
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(Self::default()),
            Err(e) => Err(e.into()),
        }
    }

    pub fn save(&self, dirs: &Dirs) -> Result<()> {
        write_private(&dirs.accounts_file(), serde_json::to_string_pretty(self)?.as_bytes())
    }

    /// Find by username (case-insensitive); with no name, the first account.
    pub fn find(&self, name: Option<&str>) -> Option<&StoredAccount> {
        match name {
            Some(n) => self.accounts.iter().find(|a| a.username.eq_ignore_ascii_case(n)),
            None => self.accounts.first(),
        }
    }

    pub fn remove(&mut self, name: &str) -> bool {
        let before = self.accounts.len();
        self.accounts.retain(|a| !a.username.eq_ignore_ascii_case(name));
        self.accounts.len() != before
    }

    fn upsert(&mut self, acct: StoredAccount) {
        match self.accounts.iter_mut().find(|a| a.uuid == acct.uuid) {
            Some(existing) => *existing = acct,
            None => self.accounts.push(acct),
        }
    }
}

fn stored_from(session: &McSession, refresh_token: String) -> StoredAccount {
    StoredAccount {
        username: session.username.clone(),
        uuid: session.uuid.clone(),
        refresh_token,
        mc_access_token: session.access_token.clone(),
        mc_expires_at: now() + session.expires_in,
    }
}

impl From<&StoredAccount> for Account {
    fn from(s: &StoredAccount) -> Self {
        Account {
            username: s.username.clone(),
            uuid: s.uuid.clone(),
            access_token: s.mc_access_token.clone(),
            user_type: "msa".into(),
        }
    }
}

/// Interactive device-code login. `show` is called once with the code to display.
pub async fn login(
    client: &reqwest::Client,
    dirs: &Dirs,
    client_id: &str,
    show: &(dyn Fn(&DeviceCode) + Send + Sync),
) -> Result<StoredAccount> {
    let dc = microsoft::request_device_code(client, client_id).await?;
    show(&dc);
    let tokens = microsoft::poll_for_token(client, client_id, &dc).await?;
    let session = microsoft::minecraft_login(client, &tokens.access_token).await?;
    let acct = stored_from(&session, tokens.refresh_token);

    let mut store = AccountStore::load(dirs)?;
    store.client_id = Some(client_id.to_string());
    store.upsert(acct.clone());
    store.save(dirs)?;
    Ok(acct)
}

/// Get a launch-ready account, refreshing the stored session if it is about to expire.
pub async fn account_for_launch(
    client: &reqwest::Client,
    dirs: &Dirs,
    name: Option<&str>,
    client_id_override: Option<&str>,
) -> Result<Account> {
    let mut store = AccountStore::load(dirs)?;
    let stored = store.find(name).cloned().ok_or_else(|| {
        Error::Other(match name {
            Some(n) => format!("no saved account named '{n}'; run `java4u account login`"),
            None => "no saved accounts; run `java4u account login` (or use --offline NAME for testing)".into(),
        })
    })?;

    if stored.mc_expires_at > now() + 120 {
        return Ok((&stored).into());
    }

    let client_id = client_id_override
        .map(String::from)
        .or_else(|| store.client_id.clone())
        .ok_or_else(|| Error::Other("no Azure client id known; pass --client-id".into()))?;
    let tokens = microsoft::refresh(client, &client_id, &stored.refresh_token).await?;
    let session = microsoft::minecraft_login(client, &tokens.access_token).await?;
    let fresh = stored_from(&session, tokens.refresh_token);
    store.upsert(fresh.clone());
    store.save(dirs)?;
    Ok((&fresh).into())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn acct(name: &str) -> StoredAccount {
        StoredAccount {
            username: name.into(),
            uuid: format!("uuid-{name}"),
            refresh_token: "r".into(),
            mc_access_token: "t".into(),
            mc_expires_at: 0,
        }
    }

    #[test]
    fn find_remove_upsert() {
        let mut s = AccountStore::default();
        s.upsert(acct("Alex"));
        s.upsert(acct("Steve"));
        let mut again = acct("Alex");
        again.mc_access_token = "new".into();
        s.upsert(again);
        assert_eq!(s.accounts.len(), 2);
        assert_eq!(s.find(Some("alex")).unwrap().mc_access_token, "new");
        assert_eq!(s.find(None).unwrap().username, "Alex");
        assert!(s.remove("STEVE"));
        assert!(!s.remove("nobody"));
    }
}
