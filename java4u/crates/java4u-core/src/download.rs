use std::collections::HashSet;
use std::path::{Path, PathBuf};
use std::time::Duration;

use futures_util::StreamExt;
use sha1::{Digest, Sha1};
use tokio::io::AsyncWriteExt;

use crate::error::{Error, Result};

const CONCURRENCY: usize = 16;
const MAX_ATTEMPTS: u32 = 3;

/// Progress callback: (stage name, completed, total).
pub type ProgressFn<'a> = &'a (dyn Fn(&str, usize, usize) + Send + Sync);

/// How to decide that an existing file is good enough to skip.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Verify {
    /// Compare SHA1 (slow, catches corruption).
    Hash,
    /// Compare file size only (fast; used for already-verified installs).
    Size,
}

#[derive(Debug, Clone)]
pub struct DownloadTask {
    pub url: String,
    pub dest: PathBuf,
    pub sha1: Option<String>,
    pub size: Option<u64>,
}

pub fn new_client() -> Result<reqwest::Client> {
    Ok(reqwest::Client::builder()
        .user_agent(concat!("java4u/", env!("CARGO_PKG_VERSION")))
        .connect_timeout(Duration::from_secs(15))
        .build()?)
}

pub async fn sha1_file(path: &Path) -> Result<String> {
    let path = path.to_owned();
    tokio::task::spawn_blocking(move || -> Result<String> {
        let mut f = std::fs::File::open(&path)?;
        let mut h = Sha1::new();
        std::io::copy(&mut f, &mut h)?;
        Ok(hex::encode(h.finalize()))
    })
    .await
    .map_err(|e| Error::Other(e.to_string()))?
}

/// True if the destination exists and matches the expected hash (or size).
async fn is_valid(task: &DownloadTask, verify: Verify) -> bool {
    let Ok(meta) = tokio::fs::metadata(&task.dest).await else {
        return false;
    };
    if !meta.is_file() {
        return false;
    }
    if verify == Verify::Size {
        return match task.size {
            Some(size) => meta.len() == size,
            None => true,
        };
    }
    if let Some(expected) = &task.sha1 {
        return match sha1_file(&task.dest).await {
            Ok(actual) => actual.eq_ignore_ascii_case(expected),
            Err(_) => false,
        };
    }
    if let Some(size) = task.size {
        return meta.len() == size;
    }
    true
}

async fn fetch(client: &reqwest::Client, task: &DownloadTask) -> Result<()> {
    if let Some(parent) = task.dest.parent() {
        tokio::fs::create_dir_all(parent).await?;
    }
    let mut part_name = task.dest.as_os_str().to_owned();
    part_name.push(".part");
    let part = PathBuf::from(part_name);

    let resp = client.get(&task.url).send().await?.error_for_status()?;
    let mut stream = resp.bytes_stream();
    let mut file = tokio::fs::File::create(&part).await?;
    let mut hasher = Sha1::new();
    while let Some(chunk) = stream.next().await {
        let chunk = chunk?;
        hasher.update(&chunk);
        file.write_all(&chunk).await?;
    }
    file.flush().await?;
    drop(file);

    if let Some(expected) = &task.sha1 {
        let actual = hex::encode(hasher.finalize());
        if !actual.eq_ignore_ascii_case(expected) {
            let _ = tokio::fs::remove_file(&part).await;
            return Err(Error::HashMismatch {
                path: task.dest.clone(),
                expected: expected.clone(),
                actual,
            });
        }
    }
    tokio::fs::rename(&part, &task.dest).await?;
    Ok(())
}

/// Download one file unless it is already valid. Retries with backoff.
pub async fn ensure(client: &reqwest::Client, task: &DownloadTask, verify: Verify) -> Result<()> {
    if is_valid(task, verify).await {
        return Ok(());
    }
    let mut attempt = 0;
    loop {
        attempt += 1;
        match fetch(client, task).await {
            Ok(()) => return Ok(()),
            Err(e) if attempt < MAX_ATTEMPTS => {
                tracing::warn!("download of {} failed (attempt {attempt}): {e}", task.url);
                tokio::time::sleep(Duration::from_millis(500 * attempt as u64)).await;
            }
            Err(e) => return Err(e),
        }
    }
}

/// Download many files concurrently, skipping ones that are already valid.
pub async fn download_all(
    client: &reqwest::Client,
    tasks: Vec<DownloadTask>,
    stage: &str,
    progress: ProgressFn<'_>,
    verify: Verify,
) -> Result<()> {
    let mut seen = HashSet::new();
    let tasks: Vec<DownloadTask> = tasks
        .into_iter()
        .filter(|t| seen.insert(t.dest.clone()))
        .collect();
    let total = tasks.len();
    progress(stage, 0, total);

    let mut stream = futures_util::stream::iter(tasks.into_iter().map(|t| {
        let client = client.clone();
        async move { ensure(&client, &t, verify).await }
    }))
    .buffer_unordered(CONCURRENCY);

    let mut done = 0;
    while let Some(res) = stream.next().await {
        res?;
        done += 1;
        progress(stage, done, total);
    }
    Ok(())
}
