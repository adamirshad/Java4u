use std::path::PathBuf;

use anyhow::{bail, Context, Result};
use clap::{Parser, Subcommand};
use indicatif::{ProgressBar, ProgressStyle};

use java4u_core::accounts::{self, AccountStore};
use java4u_core::auth::Account;
use java4u_core::download::new_client;
use java4u_core::install::install_version_with;
use java4u_core::instance::Instance;
use java4u_core::launch::{build_command, LaunchOptions};
use java4u_core::manifest::Manifest;
use java4u_core::optimize::{recommended_memory_mb, Profile};
use java4u_core::runtime::resolve_java;
use java4u_core::Dirs;

#[derive(Parser)]
#[command(name = "java4u", version, about = "A Minecraft launcher written in Rust")]
struct Cli {
    /// Where java4u stores versions, libraries, assets and instances.
    #[arg(long, global = true, env = "JAVA4U_HOME")]
    data_dir: Option<PathBuf>,

    /// Azure application (client) id used for Microsoft sign-in. Remembered after the first login.
    #[arg(long, global = true, env = "JAVA4U_CLIENT_ID")]
    client_id: Option<String>,

    #[command(subcommand)]
    cmd: Cmd,
}

#[derive(Subcommand)]
enum Cmd {
    /// List available Minecraft versions.
    List {
        /// Include snapshots.
        #[arg(long)]
        snapshots: bool,
        /// Include everything (snapshots, old betas and alphas).
        #[arg(long)]
        all: bool,
    },
    /// Download a version (client, libraries, assets). Accepts "latest".
    Install {
        version: String,
        /// Re-check every file's SHA1 (repairs corrupted files).
        #[arg(long)]
        verify: bool,
    },
    /// Manage instances.
    Instance {
        #[command(subcommand)]
        cmd: InstanceCmd,
    },
    /// Manage Microsoft accounts.
    Account {
        #[command(subcommand)]
        cmd: AccountCmd,
    },
    /// Launch an instance.
    Launch {
        instance: String,
        /// Saved Microsoft account to use (default: the first one).
        #[arg(long)]
        account: Option<String>,
        /// Play offline with this username instead (development/testing only).
        #[arg(long)]
        offline: Option<String>,
        /// Use this Java executable instead of the instance/default one.
        #[arg(long)]
        java: Option<PathBuf>,
        /// Print the command instead of running it.
        #[arg(long)]
        dry_run: bool,
    },
}

#[derive(Subcommand)]
enum AccountCmd {
    /// Sign in with Microsoft (device code flow).
    Login,
    /// List saved accounts.
    List,
    /// Remove a saved account.
    Remove { username: String },
}

#[derive(Subcommand)]
enum InstanceCmd {
    /// Create an instance for a Minecraft version (accepts "latest").
    Create { name: String, version: String },
    /// List instances.
    List,
    /// Change an instance's settings.
    Set {
        name: String,
        /// Max heap in MB.
        #[arg(long, conflicts_with = "auto_memory")]
        memory: Option<u32>,
        /// Pick a heap size that suits this machine's RAM.
        #[arg(long)]
        auto_memory: bool,
        /// JVM flag preset: off, balanced or performance.
        #[arg(long)]
        profile: Option<Profile>,
        /// Use this Java executable for the instance.
        #[arg(long)]
        java: Option<PathBuf>,
    },
}

fn progress_bar() -> ProgressBar {
    let pb = ProgressBar::new(0);
    pb.set_style(
        ProgressStyle::with_template("{msg:>12} [{bar:40.cyan/blue}] {pos}/{len}")
            .expect("valid progress template")
            .progress_chars("=> "),
    );
    pb
}

async fn run() -> Result<()> {
    let cli = Cli::parse();
    let dirs = match cli.data_dir {
        Some(p) => Dirs::new(p),
        None => Dirs::default_root()?,
    };
    let client_id = cli.client_id;
    let client = new_client()?;

    match cli.cmd {
        Cmd::List { snapshots, all } => {
            let m = Manifest::fetch(&client).await?;
            println!("latest release: {}   latest snapshot: {}\n", m.latest.release, m.latest.snapshot);
            for v in &m.versions {
                let show = all || v.kind == "release" || (snapshots && v.kind == "snapshot");
                if show {
                    println!("{:<24} {}", v.id, v.kind);
                }
            }
        }
        Cmd::Install { version, verify } => {
            let id = resolve(&client, &version).await?;
            let pb = progress_bar();
            let cb = |stage: &str, done: usize, total: usize| {
                pb.set_message(stage.to_string());
                pb.set_length(total as u64);
                pb.set_position(done as u64);
            };
            install_version_with(&client, &dirs, &id, verify, &cb).await?;
            pb.finish_and_clear();
            println!("installed {id}");
        }
        Cmd::Instance { cmd } => match cmd {
            InstanceCmd::Create { name, version } => {
                let id = resolve(&client, &version).await?;
                let inst = Instance::create(&dirs, &name, &id)?;
                println!("created instance '{}' ({})", inst.name, inst.version);
            }
            InstanceCmd::List => {
                let all = Instance::list(&dirs)?;
                if all.is_empty() {
                    println!("no instances yet; try: java4u instance create main latest");
                }
                for i in all {
                    println!("{:<24} {:<10} {} MB, {} profile", i.name, i.version, i.memory_mb, i.profile);
                }
            }
            InstanceCmd::Set { name, memory, auto_memory, profile, java } => {
                let mut inst = Instance::load(&dirs, &name)?;
                if auto_memory {
                    inst.memory_mb = recommended_memory_mb();
                }
                if let Some(m) = memory {
                    if m < 512 {
                        bail!("memory must be at least 512 MB");
                    }
                    inst.memory_mb = m;
                }
                if let Some(p) = profile {
                    inst.profile = p;
                }
                if java.is_some() {
                    inst.java_path = java;
                }
                inst.save(&dirs)?;
                println!("saved: {} MB, {} profile", inst.memory_mb, inst.profile);
            }
        },
        Cmd::Account { cmd } => match cmd {
            AccountCmd::Login => {
                let id = client_id
                    .clone()
                    .or(AccountStore::load(&dirs)?.client_id)
                    .context("pass your Azure application id with --client-id or JAVA4U_CLIENT_ID (see README)")?;
                let show = |dc: &java4u_core::microsoft::DeviceCode| {
                    println!("To sign in, open {} and enter the code: {}", dc.verification_uri, dc.user_code);
                    println!("Waiting for you to finish...");
                };
                let acct = accounts::login(&client, &dirs, &id, &show).await?;
                println!("Signed in as {} ({})", acct.username, acct.uuid);
            }
            AccountCmd::List => {
                let store = AccountStore::load(&dirs)?;
                if store.accounts.is_empty() {
                    println!("no accounts; run: java4u account login");
                }
                for a in &store.accounts {
                    println!("{:<18} {}", a.username, a.uuid);
                }
            }
            AccountCmd::Remove { username } => {
                let mut store = AccountStore::load(&dirs)?;
                if store.remove(&username) {
                    store.save(&dirs)?;
                    println!("removed {username}");
                } else {
                    bail!("no saved account named {username}");
                }
            }
        },
        Cmd::Launch { instance, account, offline, java, dry_run } => {
            let inst = Instance::load(&dirs, &instance)?;

            let pb = progress_bar();
            let cb = |stage: &str, done: usize, total: usize| {
                pb.set_message(stage.to_string());
                pb.set_length(total as u64);
                pb.set_position(done as u64);
            };
            let version = install_version_with(&client, &dirs, &inst.version, false, &cb).await?;
            let java_override = java.or_else(|| inst.java_path.clone());
            let java = resolve_java(&client, &dirs, &version, java_override.as_deref(), &cb).await?;
            pb.finish_and_clear();

            let account = match offline {
                Some(name) => {
                    eprintln!("warning: offline mode is for development and testing only");
                    Account::offline(&name)?
                }
                None => {
                    accounts::account_for_launch(&client, &dirs, account.as_deref(), client_id.as_deref())
                        .await?
                }
            };
            let opts = LaunchOptions {
                java,
                game_dir: inst.game_dir(&dirs),
                account,
                memory_mb: inst.memory_mb,
                extra_jvm_args: inst.effective_jvm_args(),
            };
            let mut cmd = build_command(&dirs, &version, &opts)?;
            if dry_run {
                println!("{cmd:?}");
            } else {
                let status = cmd.status().context("failed to start Java")?;
                if !status.success() {
                    bail!("Minecraft exited with {status}");
                }
            }
        }
    }
    Ok(())
}

async fn resolve(client: &java4u_core::reqwest::Client, id: &str) -> Result<String> {
    if matches!(id, "latest" | "release" | "snapshot") {
        Ok(Manifest::fetch(client).await?.resolve_alias(id))
    } else {
        Ok(id.to_string())
    }
}

#[tokio::main]
async fn main() {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "warn".into()),
        )
        .with_writer(std::io::stderr)
        .init();

    if let Err(e) = run().await {
        eprintln!("error: {e:#}");
        std::process::exit(1);
    }
}
