//! Download everything a version needs: client jar, libraries, asset index, assets.

use crate::download::{download_all, DownloadTask, ProgressFn, Verify};
use crate::error::{Error, Result};
use crate::manifest::Manifest;
use crate::paths::Dirs;
use crate::version::{resolve_libraries, AssetIndex, Features, VersionJson};

const RESOURCES_BASE: &str = "https://resources.download.minecraft.net";
const OK_MARKER: &str = ".java4u-ok";

/// Install (or quickly re-check) a concrete version id such as "1.21.1".
pub async fn install_version(
    client: &reqwest::Client,
    dirs: &Dirs,
    id: &str,
    progress: ProgressFn<'_>,
) -> Result<VersionJson> {
    install_version_with(client, dirs, id, false, progress).await
}

/// Like [`install_version`]. After one fully verified install a marker is written, and
/// later runs only compare file sizes (milliseconds instead of re-hashing thousands of
/// assets). `full_verify` forces SHA1 checks again, for repairing corrupted files.
pub async fn install_version_with(
    client: &reqwest::Client,
    dirs: &Dirs,
    id: &str,
    full_verify: bool,
    progress: ProgressFn<'_>,
) -> Result<VersionJson> {
    let json_path = dirs.version_json(id);
    let marker = dirs.version_dir(id).join(OK_MARKER);
    let verify = if full_verify || !marker.exists() {
        Verify::Hash
    } else {
        Verify::Size
    };

    if !json_path.exists() {
        let manifest = Manifest::fetch(client).await?;
        let entry = manifest
            .find(id)
            .ok_or_else(|| Error::Other(format!("unknown Minecraft version: {id}")))?;
        let task = DownloadTask {
            url: entry.url.clone(),
            dest: json_path.clone(),
            sha1: entry.sha1.clone(),
            size: None,
        };
        download_all(client, vec![task], "version", progress, Verify::Hash).await?;
    }
    let version = VersionJson::load(&json_path)?;

    let jar = DownloadTask {
        url: version.downloads.client.url.clone(),
        dest: dirs.version_jar(id),
        sha1: Some(version.downloads.client.sha1.clone()),
        size: Some(version.downloads.client.size),
    };
    download_all(client, vec![jar], "client", progress, verify).await?;

    let resolved = resolve_libraries(&version, &dirs.libraries_dir(), &Features::new());
    download_all(client, resolved.downloads, "libraries", progress, verify).await?;

    let idx = &version.asset_index;
    let idx_task = DownloadTask {
        url: idx.url.clone(),
        dest: dirs.asset_index(&idx.id),
        sha1: Some(idx.sha1.clone()),
        size: Some(idx.size),
    };
    download_all(client, vec![idx_task], "asset index", progress, verify).await?;

    let index = AssetIndex::load(&dirs.asset_index(&idx.id))?;
    let objects = dirs.asset_objects_dir();
    let tasks: Vec<DownloadTask> = index
        .objects
        .values()
        .map(|o| {
            let prefix = &o.hash[..2];
            DownloadTask {
                url: format!("{RESOURCES_BASE}/{prefix}/{}", o.hash),
                dest: objects.join(prefix).join(&o.hash),
                sha1: Some(o.hash.clone()),
                size: Some(o.size),
            }
        })
        .collect();
    download_all(client, tasks, "assets", progress, verify).await?;

    std::fs::write(&marker, env!("CARGO_PKG_VERSION"))?;
    Ok(version)
}
