//! Turn an installed version + account + settings into a runnable `java` command.

use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::process::Command;

use crate::auth::Account;
use crate::error::Result;
use crate::paths::Dirs;
use crate::version::{
    expand_args, extract_natives, resolve_libraries, AssetIndex, Features, VersionJson,
};

pub struct LaunchOptions {
    pub java: PathBuf,
    pub game_dir: PathBuf,
    pub account: Account,
    pub memory_mb: u32,
    pub extra_jvm_args: Vec<String>,
}

fn substitute(s: &str, vars: &HashMap<&str, String>) -> String {
    let mut out = s.to_string();
    for (k, v) in vars {
        out = out.replace(&format!("${{{k}}}"), v);
    }
    out
}

/// Copy hashed asset objects to human-readable paths (pre-1.7 "virtual" assets
/// and pre-1.6 "map_to_resources").
fn sync_assets(index: &AssetIndex, objects: &Path, target: &Path) -> Result<()> {
    for (name, obj) in &index.objects {
        let dst = target.join(name);
        if dst.exists() {
            continue;
        }
        let src = objects.join(&obj.hash[..2]).join(&obj.hash);
        if let Some(parent) = dst.parent() {
            std::fs::create_dir_all(parent)?;
        }
        std::fs::copy(&src, &dst)?;
    }
    Ok(())
}

pub fn build_command(dirs: &Dirs, v: &VersionJson, opts: &LaunchOptions) -> Result<Command> {
    let features = Features::new();

    // Classpath + natives
    let resolved = resolve_libraries(v, &dirs.libraries_dir(), &features);
    let mut cp = resolved.classpath.clone();
    cp.push(dirs.version_jar(&v.id));
    let sep = if cfg!(windows) { ";" } else { ":" };
    let classpath = cp
        .iter()
        .map(|p| p.to_string_lossy().into_owned())
        .collect::<Vec<_>>()
        .join(sep);
    let natives_dir = dirs.natives_dir(&v.id);
    extract_natives(&resolved.natives, &natives_dir)?;

    // Assets
    let index = AssetIndex::load(&dirs.asset_index(&v.asset_index.id))?;
    std::fs::create_dir_all(&opts.game_dir)?;
    let assets_root = dirs.assets_dir();
    let mut game_assets = assets_root.clone();
    if index.is_virtual {
        let target = assets_root.join("virtual").join(&v.asset_index.id);
        sync_assets(&index, &dirs.asset_objects_dir(), &target)?;
        game_assets = target;
    }
    if index.map_to_resources {
        sync_assets(&index, &dirs.asset_objects_dir(), &opts.game_dir.join("resources"))?;
    }

    let s = |p: &Path| p.to_string_lossy().into_owned();
    let mut vars: HashMap<&str, String> = HashMap::new();
    vars.insert("auth_player_name", opts.account.username.clone());
    vars.insert("auth_uuid", opts.account.uuid.clone());
    vars.insert("auth_access_token", opts.account.access_token.clone());
    vars.insert("auth_session", format!("token:{}:{}", opts.account.access_token, opts.account.uuid));
    vars.insert("user_type", opts.account.user_type.clone());
    vars.insert("user_properties", "{}".into());
    vars.insert("clientid", String::new());
    vars.insert("auth_xuid", String::new());
    vars.insert("version_name", v.id.clone());
    vars.insert("version_type", v.kind.clone());
    vars.insert("game_directory", s(&opts.game_dir));
    vars.insert("assets_root", s(&assets_root));
    vars.insert("game_assets", s(&game_assets));
    vars.insert(
        "assets_index_name",
        v.assets.clone().unwrap_or_else(|| v.asset_index.id.clone()),
    );
    vars.insert("natives_directory", s(&natives_dir));
    vars.insert("library_directory", s(&dirs.libraries_dir()));
    vars.insert("classpath_separator", sep.into());
    vars.insert("classpath", classpath);
    vars.insert("launcher_name", "java4u".into());
    vars.insert("launcher_version", env!("CARGO_PKG_VERSION").into());

    // JVM + game argument templates (modern `arguments` or legacy string)
    let (jvm_tpl, game_tpl): (Vec<String>, Vec<String>) = match &v.arguments {
        Some(a) => (expand_args(&a.jvm, &features), expand_args(&a.game, &features)),
        None => (
            vec![
                "-Djava.library.path=${natives_directory}".into(),
                "-cp".into(),
                "${classpath}".into(),
            ],
            v.minecraft_arguments
                .as_deref()
                .unwrap_or_default()
                .split_whitespace()
                .map(String::from)
                .collect(),
        ),
    };

    let mut cmd = Command::new(&opts.java);
    cmd.arg(format!("-Xmx{}M", opts.memory_mb));
    cmd.args(&opts.extra_jvm_args);
    cmd.args(jvm_tpl.iter().map(|a| substitute(a, &vars)));
    cmd.arg(&v.main_class);
    cmd.args(game_tpl.iter().map(|a| substitute(a, &vars)));
    cmd.current_dir(&opts.game_dir);
    Ok(cmd)
}
