//! Download and manage the Java runtimes Mojang publishes for each Minecraft version.

use std::collections::HashMap;
use std::path::{Component, Path, PathBuf};

use serde::Deserialize;
use sha1::{Digest, Sha1};

use crate::download::{download_all, DownloadTask, ProgressFn, Verify};
use crate::error::{Error, Result};
use crate::java::{check_java, find_java};
use crate::paths::Dirs;
use crate::version::{Artifact, VersionJson};

const RUNTIME_INDEX_URL: &str = "https://launchermeta.mojang.com/v1/products/java-runtime/2ec0cc96c44e5a76b9c8b7c39df7210883d12871/all.json";
const OK_MARKER: &str = ".java4u-ok";

type RuntimeIndex = HashMap<String, HashMap<String, Vec<RuntimeEntry>>>;

#[derive(Deserialize)]
struct RuntimeEntry {
    manifest: Artifact,
}

#[derive(Deserialize)]
struct RuntimeManifest {
    files: HashMap<String, RuntimeFile>,
}

#[derive(Deserialize)]
struct RuntimeFile {
    #[serde(rename = "type")]
    kind: String,
    downloads: Option<RuntimeDownloads>,
    #[serde(default)]
    executable: bool,
    target: Option<String>,
}

#[derive(Deserialize)]
struct RuntimeDownloads {
    raw: Option<Artifact>,
}

fn platform_key() -> Option<&'static str> {
    match (std::env::consts::OS, std::env::consts::ARCH) {
        ("linux", "x86_64") => Some("linux"),
        ("linux", "x86") => Some("linux-i386"),
        ("macos", "x86_64") => Some("mac-os"),
        ("macos", "aarch64") => Some("mac-os-arm64"),
        ("windows", "x86_64") => Some("windows-x64"),
        ("windows", "aarch64") => Some("windows-arm64"),
        ("windows", "x86") => Some("windows-x86"),
        _ => None,
    }
}

fn java_binary(root: &Path) -> Option<PathBuf> {
    let exe = if cfg!(windows) { "java.exe" } else { "java" };
    [
        root.join("bin").join(exe),
        root.join("jre.bundle/Contents/Home/bin").join(exe),
        root.join("Contents/Home/bin").join(exe),
    ]
    .into_iter()
    .find(|p| p.exists())
}

/// Reject absolute paths and `..` so a malicious manifest cannot escape the runtime dir.
fn safe_rel(path: &str) -> Result<PathBuf> {
    let p = Path::new(path);
    let bad = p.is_absolute()
        || p.components().any(|c| !matches!(c, Component::Normal(_) | Component::CurDir));
    if bad {
        return Err(Error::Other(format!("unsafe path in runtime manifest: {path}")));
    }
    Ok(p.to_path_buf())
}

/// Make sure the named runtime component (e.g. "java-runtime-delta") is installed and
/// return the path to its `java` executable. Once installed this does no network I/O.
pub async fn ensure_runtime(
    client: &reqwest::Client,
    dirs: &Dirs,
    component: &str,
    progress: ProgressFn<'_>,
) -> Result<PathBuf> {
    let platform = platform_key().ok_or_else(|| {
        Error::Other("Mojang publishes no Java runtime for this platform; install Java yourself".into())
    })?;
    let root = dirs.runtimes_dir().join(component).join(platform);
    let marker = root.join(OK_MARKER);

    if marker.exists() {
        if let Some(java) = java_binary(&root) {
            return Ok(java);
        }
    }

    let index: RuntimeIndex = client
        .get(RUNTIME_INDEX_URL)
        .send()
        .await?
        .error_for_status()?
        .json()
        .await?;
    let entry = index
        .get(platform)
        .and_then(|m| m.get(component))
        .and_then(|v| v.first())
        .ok_or_else(|| {
            Error::Other(format!("Mojang has no runtime '{component}' for {platform}"))
        })?;

    let bytes = client
        .get(&entry.manifest.url)
        .send()
        .await?
        .error_for_status()?
        .bytes()
        .await?;
    let actual = hex::encode(Sha1::digest(&bytes));
    if !actual.eq_ignore_ascii_case(&entry.manifest.sha1) {
        return Err(Error::HashMismatch {
            path: PathBuf::from("runtime manifest"),
            expected: entry.manifest.sha1.clone(),
            actual,
        });
    }
    let manifest: RuntimeManifest = serde_json::from_slice(&bytes)?;

    let mut tasks = Vec::new();
    let mut executables = Vec::new();
    let mut links = Vec::new();
    for (path, file) in &manifest.files {
        let rel = safe_rel(path)?;
        match file.kind.as_str() {
            "directory" => std::fs::create_dir_all(root.join(&rel))?,
            "file" => {
                if let Some(raw) = file.downloads.as_ref().and_then(|d| d.raw.as_ref()) {
                    tasks.push(DownloadTask {
                        url: raw.url.clone(),
                        dest: root.join(&rel),
                        sha1: Some(raw.sha1.clone()),
                        size: Some(raw.size),
                    });
                    if file.executable {
                        executables.push(root.join(&rel));
                    }
                }
            }
            "link" => {
                if let Some(target) = &file.target {
                    links.push((root.join(&rel), target.clone()));
                }
            }
            _ => {}
        }
    }

    download_all(client, tasks, "java runtime", progress, Verify::Hash).await?;

    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        for path in &executables {
            std::fs::set_permissions(path, std::fs::Permissions::from_mode(0o755))?;
        }
        for (link, target) in &links {
            if let Some(parent) = link.parent() {
                std::fs::create_dir_all(parent)?;
            }
            let _ = std::fs::remove_file(link);
            std::os::unix::fs::symlink(target, link)?;
        }
    }
    #[cfg(not(unix))]
    {
        let _ = (&executables, &links);
    }

    let java = java_binary(&root)
        .ok_or_else(|| Error::Other("downloaded runtime has no java executable".into()))?;
    std::fs::write(&marker, &entry.manifest.sha1)?;
    Ok(java)
}

/// Pick the Java to run a version with. Order: explicit override, Mojang's runtime for
/// that version (downloaded on first use), then a suitable system Java.
pub async fn resolve_java(
    client: &reqwest::Client,
    dirs: &Dirs,
    version: &VersionJson,
    override_path: Option<&Path>,
    progress: ProgressFn<'_>,
) -> Result<PathBuf> {
    let (component, major) = match &version.java_version {
        Some(j) => (j.component.as_str(), j.major_version),
        None => ("jre-legacy", 8),
    };

    if let Some(path) = override_path {
        check_java(path, major)?;
        return Ok(path.to_path_buf());
    }

    match ensure_runtime(client, dirs, component, progress).await {
        Ok(path) => Ok(path),
        Err(e) => {
            tracing::warn!("could not set up Mojang's Java runtime: {e}");
            let system = find_java();
            check_java(&system, major).map_err(|_| {
                Error::Other(format!(
                    "could not set up Java {major} ({e}) and no suitable system Java was found"
                ))
            })?;
            Ok(system)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::safe_rel;

    #[test]
    fn rejects_escaping_paths() {
        assert!(safe_rel("bin/java").is_ok());
        assert!(safe_rel("../evil").is_err());
        assert!(safe_rel("a/../../evil").is_err());
        assert!(safe_rel("/etc/passwd").is_err());
    }
}
