//! Accounts. Only offline accounts exist for now (development/testing).
//! Microsoft sign-in (device code -> Xbox -> XSTS -> Minecraft) is the next milestone.

use serde::{Deserialize, Serialize};

use crate::error::{Error, Result};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Account {
    pub username: String,
    /// 32-char hex UUID without dashes.
    pub uuid: String,
    pub access_token: String,
    /// "msa" for Microsoft accounts, "legacy" for offline.
    pub user_type: String,
}

impl Account {
    /// Offline account with the same UUID the vanilla server derives for offline mode.
    pub fn offline(name: &str) -> Result<Self> {
        let valid = (3..=16).contains(&name.len())
            && name.chars().all(|c| c.is_ascii_alphanumeric() || c == '_');
        if !valid {
            return Err(Error::Other(
                "username must be 3-16 characters: letters, digits or underscore".into(),
            ));
        }
        Ok(Self {
            username: name.to_string(),
            uuid: offline_uuid(name),
            access_token: "0".into(),
            user_type: "legacy".into(),
        })
    }
}

/// Equivalent of Java's UUID.nameUUIDFromBytes("OfflinePlayer:<name>").
pub fn offline_uuid(name: &str) -> String {
    let digest = md5::compute(format!("OfflinePlayer:{name}"));
    let mut b = digest.0;
    b[6] = (b[6] & 0x0f) | 0x30;
    b[8] = (b[8] & 0x3f) | 0x80;
    uuid::Uuid::from_bytes(b).simple().to_string()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn offline_uuid_shape() {
        let u = offline_uuid("Steve");
        assert_eq!(u.len(), 32);
        assert_eq!(&u[12..13], "3");
        assert_eq!(u, offline_uuid("Steve"));
        assert_ne!(u, offline_uuid("Alex"));
    }

    #[test]
    fn rejects_bad_names() {
        assert!(Account::offline("ab").is_err());
        assert!(Account::offline("has space").is_err());
        assert!(Account::offline("Steve_1").is_ok());
    }
}
