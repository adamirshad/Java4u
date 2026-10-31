//! Models for Mojang's per-version JSON, rule evaluation, and library resolution.

use std::collections::HashMap;
use std::io;
use std::path::{Path, PathBuf};

use serde::Deserialize;

use crate::download::DownloadTask;
use crate::error::{Error, Result};

pub const LIBRARIES_BASE: &str = "https://libraries.minecraft.net/";

#[derive(Debug, Clone, Deserialize)]
pub struct Artifact {
    pub sha1: String,
    pub size: u64,
    pub url: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct LibArtifact {
    pub path: String,
    pub sha1: String,
    pub size: u64,
    pub url: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct AssetIndexRef {
    pub id: String,
    pub sha1: String,
    pub size: u64,
    pub url: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct JavaVersion {
    pub component: String,
    #[serde(rename = "majorVersion")]
    pub major_version: u32,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Downloads {
    pub client: Artifact,
}

#[derive(Debug, Clone, Deserialize)]
pub struct VersionJson {
    pub id: String,
    #[serde(rename = "type")]
    pub kind: String,
    #[serde(rename = "mainClass")]
    pub main_class: String,
    #[serde(rename = "assetIndex")]
    pub asset_index: AssetIndexRef,
    pub assets: Option<String>,
    pub downloads: Downloads,
    #[serde(default)]
    pub libraries: Vec<Library>,
    pub arguments: Option<Arguments>,
    #[serde(rename = "minecraftArguments")]
    pub minecraft_arguments: Option<String>,
    #[serde(rename = "javaVersion")]
    pub java_version: Option<JavaVersion>,
    #[serde(rename = "inheritsFrom")]
    pub inherits_from: Option<String>,
}

impl VersionJson {
    pub fn load(path: &Path) -> Result<Self> {
        let text = std::fs::read_to_string(path)?;
        let v: VersionJson = serde_json::from_str(&text)?;
        if v.inherits_from.is_some() {
            return Err(Error::Other(
                "versions that use `inheritsFrom` (mod loaders) are not supported yet".into(),
            ));
        }
        Ok(v)
    }
}

#[derive(Debug, Clone, Deserialize)]
pub struct Library {
    pub name: String,
    pub downloads: Option<LibDownloads>,
    #[serde(default)]
    pub rules: Vec<Rule>,
    pub natives: Option<HashMap<String, String>>,
    pub extract: Option<Extract>,
    pub url: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct LibDownloads {
    pub artifact: Option<LibArtifact>,
    pub classifiers: Option<HashMap<String, LibArtifact>>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Extract {
    #[serde(default)]
    pub exclude: Vec<String>,
}

// ---------------------------------------------------------------- rules

#[derive(Debug, Clone, Deserialize)]
pub struct Rule {
    pub action: String,
    pub os: Option<OsRule>,
    pub features: Option<HashMap<String, bool>>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct OsRule {
    pub name: Option<String>,
    pub arch: Option<String>,
}

pub type Features = HashMap<String, bool>;

pub fn current_os() -> &'static str {
    if cfg!(target_os = "windows") {
        "windows"
    } else if cfg!(target_os = "macos") {
        "osx"
    } else {
        "linux"
    }
}

pub fn current_arch() -> &'static str {
    match std::env::consts::ARCH {
        "x86_64" => "x86_64",
        "aarch64" => "arm64",
        "x86" => "x86",
        other => other,
    }
}

fn arch_bits() -> &'static str {
    if cfg!(target_pointer_width = "64") {
        "64"
    } else {
        "32"
    }
}

impl Rule {
    fn matches(&self, features: &Features) -> bool {
        if let Some(os) = &self.os {
            if let Some(name) = &os.name {
                if name != current_os() {
                    return false;
                }
            }
            if let Some(arch) = &os.arch {
                if arch != current_arch() {
                    return false;
                }
            }
        }
        if let Some(required) = &self.features {
            for (key, want) in required {
                if features.get(key).copied().unwrap_or(false) != *want {
                    return false;
                }
            }
        }
        true
    }
}

/// Mojang semantics: no rules = allowed; otherwise start disallowed and the
/// last matching rule wins.
pub fn rules_allow(rules: &[Rule], features: &Features) -> bool {
    if rules.is_empty() {
        return true;
    }
    let mut allowed = false;
    for rule in rules {
        if rule.matches(features) {
            allowed = rule.action == "allow";
        }
    }
    allowed
}

// ------------------------------------------------------------ arguments

#[derive(Debug, Clone, Default, Deserialize)]
pub struct Arguments {
    #[serde(default)]
    pub game: Vec<Arg>,
    #[serde(default)]
    pub jvm: Vec<Arg>,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(untagged)]
pub enum Arg {
    Plain(String),
    Conditional {
        #[serde(default)]
        rules: Vec<Rule>,
        value: OneOrMany,
    },
}

#[derive(Debug, Clone, Deserialize)]
#[serde(untagged)]
pub enum OneOrMany {
    One(String),
    Many(Vec<String>),
}

pub fn expand_args(args: &[Arg], features: &Features) -> Vec<String> {
    let mut out = Vec::new();
    for arg in args {
        match arg {
            Arg::Plain(s) => out.push(s.clone()),
            Arg::Conditional { rules, value } => {
                if rules_allow(rules, features) {
                    match value {
                        OneOrMany::One(s) => out.push(s.clone()),
                        OneOrMany::Many(v) => out.extend(v.iter().cloned()),
                    }
                }
            }
        }
    }
    out
}

// --------------------------------------------------------------- assets

#[derive(Debug, Clone, Deserialize)]
pub struct AssetIndex {
    pub objects: HashMap<String, AssetObject>,
    #[serde(rename = "virtual", default)]
    pub is_virtual: bool,
    #[serde(default)]
    pub map_to_resources: bool,
}

#[derive(Debug, Clone, Deserialize)]
pub struct AssetObject {
    pub hash: String,
    pub size: u64,
}

impl AssetIndex {
    pub fn load(path: &Path) -> Result<Self> {
        Ok(serde_json::from_str(&std::fs::read_to_string(path)?)?)
    }
}

// ------------------------------------------------------------ libraries

#[derive(Debug, Clone)]
pub struct NativeJar {
    pub path: PathBuf,
    pub exclude: Vec<String>,
}

#[derive(Debug, Default)]
pub struct ResolvedLibraries {
    pub classpath: Vec<PathBuf>,
    pub downloads: Vec<DownloadTask>,
    pub natives: Vec<NativeJar>,
}

fn maven_path(name: &str) -> Option<String> {
    let parts: Vec<&str> = name.split(':').collect();
    if parts.len() < 3 {
        return None;
    }
    let group = parts[0].replace('.', "/");
    let (artifact, version) = (parts[1], parts[2]);
    let classifier = parts.get(3).map(|c| format!("-{c}")).unwrap_or_default();
    Some(format!("{group}/{artifact}/{version}/{artifact}-{version}{classifier}.jar"))
}

pub fn resolve_libraries(
    v: &VersionJson,
    libs_dir: &Path,
    features: &Features,
) -> ResolvedLibraries {
    let mut out = ResolvedLibraries::default();

    for lib in &v.libraries {
        if !rules_allow(&lib.rules, features) {
            continue;
        }
        let dls = lib.downloads.as_ref();

        if let Some(a) = dls.and_then(|d| d.artifact.as_ref()) {
            let dest = libs_dir.join(&a.path);
            out.downloads.push(DownloadTask {
                url: a.url.clone(),
                dest: dest.clone(),
                sha1: Some(a.sha1.clone()),
                size: Some(a.size),
            });
            out.classpath.push(dest);
        } else if lib.natives.is_none() && dls.is_none() {
            // Old maven-style entry without a downloads block.
            if let Some(rel) = maven_path(&lib.name) {
                let mut base = lib.url.clone().unwrap_or_else(|| LIBRARIES_BASE.to_string());
                if !base.ends_with('/') {
                    base.push('/');
                }
                let dest = libs_dir.join(&rel);
                out.downloads.push(DownloadTask {
                    url: format!("{base}{rel}"),
                    dest: dest.clone(),
                    sha1: None,
                    size: None,
                });
                out.classpath.push(dest);
            }
        }

        // Legacy natives (pre-1.19): classifier jars extracted to a natives dir.
        if let Some(map) = &lib.natives {
            if let Some(template) = map.get(current_os()) {
                let key = template.replace("${arch}", arch_bits());
                if let Some(c) = dls
                    .and_then(|d| d.classifiers.as_ref())
                    .and_then(|c| c.get(&key))
                {
                    let dest = libs_dir.join(&c.path);
                    out.downloads.push(DownloadTask {
                        url: c.url.clone(),
                        dest: dest.clone(),
                        sha1: Some(c.sha1.clone()),
                        size: Some(c.size),
                    });
                    out.natives.push(NativeJar {
                        path: dest,
                        exclude: lib.extract.as_ref().map(|e| e.exclude.clone()).unwrap_or_default(),
                    });
                }
            }
        }
    }

    let mut seen = std::collections::HashSet::new();
    out.classpath.retain(|p| seen.insert(p.clone()));
    out
}

pub fn extract_natives(jars: &[NativeJar], dest: &Path) -> Result<()> {
    std::fs::create_dir_all(dest)?;
    for nj in jars {
        let file = std::fs::File::open(&nj.path)?;
        let mut zip = zip::ZipArchive::new(file)?;
        for i in 0..zip.len() {
            let mut entry = zip.by_index(i)?;
            if entry.is_dir() {
                continue;
            }
            let name = entry.name().to_string();
            if nj.exclude.iter().any(|x| name.starts_with(x.as_str())) {
                continue;
            }
            let Some(rel) = entry.enclosed_name().map(|p| p.to_path_buf()) else {
                continue;
            };
            let out_path = dest.join(rel);
            if let Some(parent) = out_path.parent() {
                std::fs::create_dir_all(parent)?;
            }
            let mut out = std::fs::File::create(&out_path)?;
            io::copy(&mut entry, &mut out)?;
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn rule(action: &str, os: Option<&str>) -> Rule {
        Rule {
            action: action.into(),
            os: os.map(|n| OsRule { name: Some(n.into()), arch: None }),
            features: None,
        }
    }

    #[test]
    fn empty_rules_allow() {
        assert!(rules_allow(&[], &Features::new()));
    }

    #[test]
    fn last_matching_rule_wins() {
        let rules = vec![rule("allow", None), rule("disallow", Some(current_os()))];
        assert!(!rules_allow(&rules, &Features::new()));
    }

    #[test]
    fn non_matching_os_rule_is_disallowed() {
        let other = if current_os() == "windows" { "linux" } else { "windows" };
        assert!(!rules_allow(&[rule("allow", Some(other))], &Features::new()));
    }

    #[test]
    fn feature_rules_default_to_false() {
        let mut f = HashMap::new();
        f.insert("is_demo_user".to_string(), true);
        let r = Rule { action: "allow".into(), os: None, features: Some(f) };
        assert!(!rules_allow(&[r.clone()], &Features::new()));
        let mut on = Features::new();
        on.insert("is_demo_user".into(), true);
        assert!(rules_allow(&[r], &on));
    }

    #[test]
    fn maven_paths() {
        assert_eq!(
            maven_path("net.sf.jopt-simple:jopt-simple:5.0.4").unwrap(),
            "net/sf/jopt-simple/jopt-simple/5.0.4/jopt-simple-5.0.4.jar"
        );
        assert!(maven_path("bad").is_none());
    }

    #[test]
    fn parses_mixed_arguments() {
        let json = r#"{"game":["--username",{"rules":[{"action":"allow","features":{"is_demo_user":true}}],"value":"--demo"},{"rules":[],"value":["a","b"]}],"jvm":[]}"#;
        let args: Arguments = serde_json::from_str(json).unwrap();
        assert_eq!(expand_args(&args.game, &Features::new()), vec!["--username", "a", "b"]);
    }
}
