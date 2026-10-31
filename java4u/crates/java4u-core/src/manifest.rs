use serde::Deserialize;

use crate::error::Result;

pub const MANIFEST_URL: &str = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json";

#[derive(Debug, Clone, Deserialize)]
pub struct Manifest {
    pub latest: Latest,
    pub versions: Vec<VersionEntry>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Latest {
    pub release: String,
    pub snapshot: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct VersionEntry {
    pub id: String,
    #[serde(rename = "type")]
    pub kind: String,
    pub url: String,
    pub sha1: Option<String>,
    #[serde(rename = "releaseTime")]
    pub release_time: String,
}

impl Manifest {
    pub async fn fetch(client: &reqwest::Client) -> Result<Self> {
        Ok(client
            .get(MANIFEST_URL)
            .send()
            .await?
            .error_for_status()?
            .json()
            .await?)
    }

    pub fn find(&self, id: &str) -> Option<&VersionEntry> {
        self.versions.iter().find(|v| v.id == id)
    }

    /// Turn "latest"/"release"/"snapshot" into a concrete version id.
    pub fn resolve_alias(&self, id: &str) -> String {
        match id {
            "latest" | "release" => self.latest.release.clone(),
            "snapshot" => self.latest.snapshot.clone(),
            other => other.to_string(),
        }
    }
}
