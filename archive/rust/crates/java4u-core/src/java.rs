use std::path::{Path, PathBuf};
use std::process::Command;

use crate::error::{Error, Result};

/// JAVA_HOME/bin/java if set, otherwise `java` from PATH.
pub fn find_java() -> PathBuf {
    let exe = if cfg!(windows) { "java.exe" } else { "java" };
    if let Some(home) = std::env::var_os("JAVA_HOME") {
        let p = PathBuf::from(home).join("bin").join(exe);
        if p.exists() {
            return p;
        }
    }
    PathBuf::from(exe)
}

/// Parse the major version out of `java -version` output.
pub fn java_major(java: &Path) -> Result<u32> {
    let out = Command::new(java)
        .arg("-version")
        .output()
        .map_err(|e| Error::Other(format!("could not run {}: {e}", java.display())))?;
    let text = String::from_utf8_lossy(&out.stderr);
    parse_major(&text).ok_or_else(|| Error::Other("could not parse `java -version` output".into()))
}

fn parse_major(text: &str) -> Option<u32> {
    let start = text.find('"')?;
    let rest = &text[start + 1..];
    let ver = &rest[..rest.find('"')?];
    let major = match ver.strip_prefix("1.") {
        Some(r) => r.split('.').next()?,
        None => ver.split(|c: char| !c.is_ascii_digit()).next()?,
    };
    major.parse().ok()
}

pub fn check_java(java: &Path, required: u32) -> Result<()> {
    let have = java_major(java)?;
    if have < required {
        return Err(Error::Other(format!(
            "this Minecraft version needs Java {required}+, but {} is Java {have}",
            java.display()
        )));
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::parse_major;

    #[test]
    fn parses_versions() {
        assert_eq!(parse_major("openjdk version \"21.0.2\" 2024-01-16"), Some(21));
        assert_eq!(parse_major("java version \"1.8.0_312\""), Some(8));
        assert_eq!(parse_major("openjdk version \"17\" 2021"), Some(17));
        assert_eq!(parse_major("openjdk version \"23-ea\""), Some(23));
        assert_eq!(parse_major("nonsense"), None);
    }
}
