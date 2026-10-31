use std::io::{BufRead, BufReader};
use std::path::PathBuf;
use std::process::Stdio;
use std::sync::{Arc, Mutex};

use eframe::egui::{self, Color32, RichText};
use java4u_core::accounts::{self, AccountStore};
use java4u_core::auth::Account;
use java4u_core::download::new_client;
use java4u_core::install::install_version;
use java4u_core::instance::Instance;
use java4u_core::launch::{build_command, LaunchOptions};
use java4u_core::manifest::{Manifest, VersionEntry};
use java4u_core::microsoft::DeviceCode;
use java4u_core::optimize::{self, Profile};
use java4u_core::runtime::resolve_java;
use java4u_core::settings::Settings;
use java4u_core::{wallpaper, Dirs, Error};

const MAX_LOG_LINES: usize = 3000;

#[derive(Clone, Copy, PartialEq)]
enum Tab {
    Play,
    Instances,
    Accounts,
    Appearance,
}

/// State written by background tasks and read by the UI thread.
#[derive(Default)]
struct Shared {
    log: Vec<String>,
    status: String,
    progress: Option<(String, usize, usize)>,
    busy: bool,
    running: bool,
    logging_in: bool,
    device_code: Option<DeviceCode>,
    versions: Vec<VersionEntry>,
    accounts_dirty: bool,
    imported: Option<String>,
    wallpaper_pending: Option<(String, u32, u32, Vec<u8>)>,
}

type Sh = Arc<Mutex<Shared>>;

fn log(sh: &Sh, msg: impl Into<String>) {
    let mut g = sh.lock().unwrap();
    g.log.push(msg.into());
    if g.log.len() > MAX_LOG_LINES {
        let extra = g.log.len() - MAX_LOG_LINES;
        g.log.drain(0..extra);
    }
}

fn set_status(sh: &Sh, msg: impl Into<String>) {
    sh.lock().unwrap().status = msg.into();
}

pub struct App {
    dirs: Dirs,
    rt: tokio::runtime::Runtime,
    client: java4u_core::reqwest::Client,
    sh: Sh,
    settings: Settings,
    instances: Vec<Instance>,
    selected: Option<usize>,
    accounts: AccountStore,
    selected_account: Option<String>,
    tab: Tab,
    use_offline: bool,
    // wallpaper
    wallpaper_tex: Option<egui::TextureHandle>,
    wallpaper_loaded: Option<String>,
    wallpaper_list: Vec<String>,
    dim_dirty: bool,
    // forms
    new_name: String,
    new_version: String,
    args_buf: String,
    args_for: Option<usize>,
    confirm_delete: bool,
    client_id_input: String,
}

impl App {
    pub fn new(cc: &eframe::CreationContext<'_>) -> Self {
        let mut visuals = egui::Visuals::dark();
        visuals.panel_fill = Color32::from_rgba_unmultiplied(14, 16, 24, 190);
        visuals.window_fill = Color32::from_rgba_unmultiplied(20, 22, 32, 235);
        cc.egui_ctx.set_visuals(visuals);

        let dirs = Dirs::default_root().expect("could not find a data directory");
        let settings = Settings::load(&dirs);
        let instances = Instance::list(&dirs).unwrap_or_default();
        let accounts = AccountStore::load(&dirs).unwrap_or_default();
        let selected = settings
            .last_instance
            .as_ref()
            .and_then(|n| instances.iter().position(|i| &i.name == n))
            .or(if instances.is_empty() { None } else { Some(0) });
        let selected_account = accounts.accounts.first().map(|a| a.username.clone());
        let wallpaper_list = wallpaper::list(&dirs).unwrap_or_default();

        let rt = tokio::runtime::Runtime::new().expect("could not start async runtime");
        let client = new_client().expect("could not create HTTP client");
        let sh: Sh = Arc::new(Mutex::new(Shared::default()));

        // Fetch the version list in the background.
        {
            let (client, sh, ctx) = (client.clone(), sh.clone(), cc.egui_ctx.clone());
            rt.spawn(async move {
                match Manifest::fetch(&client).await {
                    Ok(m) => sh.lock().unwrap().versions = m.versions,
                    Err(e) => set_status(&sh, format!("Could not load the version list: {e}")),
                }
                ctx.request_repaint();
            });
        }

        Self {
            dirs,
            rt,
            client,
            sh,
            settings,
            instances,
            selected,
            accounts,
            selected_account,
            tab: Tab::Play,
            use_offline: false,
            wallpaper_tex: None,
            wallpaper_loaded: None,
            wallpaper_list,
            dim_dirty: false,
            new_name: String::new(),
            new_version: String::new(),
            args_buf: String::new(),
            args_for: None,
            confirm_delete: false,
            client_id_input: String::new(),
        }
    }

    // ------------------------------------------------------------ plumbing

    fn reload_instances(&mut self) {
        self.instances = Instance::list(&self.dirs).unwrap_or_default();
        if self.instances.is_empty() {
            self.selected = None;
        } else if self.selected.map_or(true, |i| i >= self.instances.len()) {
            self.selected = Some(0);
        }
        self.args_for = None;
    }

    fn reload_accounts(&mut self) {
        self.accounts = AccountStore::load(&self.dirs).unwrap_or_default();
        let still_there = self
            .selected_account
            .as_ref()
            .map_or(false, |n| self.accounts.accounts.iter().any(|a| &a.username == n));
        if !still_there {
            self.selected_account = self.accounts.accounts.first().map(|a| a.username.clone());
        }
    }

    fn start_wallpaper_load(&self, ctx: &egui::Context) {
        let Some(name) = self.settings.wallpaper.clone() else {
            return;
        };
        let Ok(path) = wallpaper::path_of(&self.dirs, &name) else {
            return;
        };
        let max_side = ctx.input(|i| i.max_texture_side) as u32;
        let (sh, ctx) = (self.sh.clone(), ctx.clone());
        std::thread::spawn(move || {
            match wallpaper::load_rgba(&path, max_side) {
                Ok((w, h, px)) => sh.lock().unwrap().wallpaper_pending = Some((name, w, h, px)),
                Err(e) => set_status(&sh, format!("Could not load wallpaper: {e}")),
            }
            ctx.request_repaint();
        });
    }

    fn import_wallpaper_async(&self, ctx: &egui::Context, path: PathBuf) {
        let (dirs, sh, ctx) = (self.dirs.clone(), self.sh.clone(), ctx.clone());
        set_status(&sh, "Importing wallpaper...");
        std::thread::spawn(move || {
            match wallpaper::import(&dirs, &path) {
                Ok(name) => {
                    let mut g = sh.lock().unwrap();
                    g.imported = Some(name);
                    g.status = "Wallpaper added.".into();
                }
                Err(e) => set_status(&sh, format!("Could not import that image: {e}")),
            }
            ctx.request_repaint();
        });
    }

    /// Pull results from background tasks into UI state. Runs every frame.
    fn poll(&mut self, ctx: &egui::Context) {
        let pending = self.sh.lock().unwrap().wallpaper_pending.take();
        if let Some((name, w, h, px)) = pending {
            if self.settings.wallpaper.as_deref() == Some(name.as_str()) {
                let img = egui::ColorImage::from_rgba_unmultiplied([w as usize, h as usize], &px);
                self.wallpaper_tex =
                    Some(ctx.load_texture("wallpaper", img, egui::TextureOptions::LINEAR));
            }
        }

        let imported = self.sh.lock().unwrap().imported.take();
        if let Some(name) = imported {
            self.settings.wallpaper = Some(name);
            let _ = self.settings.save(&self.dirs);
            self.wallpaper_list = wallpaper::list(&self.dirs).unwrap_or_default();
        }

        let accounts_dirty = std::mem::take(&mut self.sh.lock().unwrap().accounts_dirty);
        if accounts_dirty {
            self.reload_accounts();
        }

        if self.settings.wallpaper != self.wallpaper_loaded {
            self.wallpaper_loaded = self.settings.wallpaper.clone();
            if self.settings.wallpaper.is_some() {
                self.start_wallpaper_load(ctx);
            } else {
                self.wallpaper_tex = None;
            }
        }

        // Drag and drop an image anywhere on the window to use it as wallpaper.
        let dropped = ctx.input(|i| i.raw.dropped_files.clone());
        for file in dropped {
            if let Some(path) = file.path {
                self.import_wallpaper_async(ctx, path);
            }
        }
    }

    fn paint_background(&self, ctx: &egui::Context) {
        let screen = ctx.screen_rect();
        let painter = ctx.layer_painter(egui::LayerId::background());
        match &self.wallpaper_tex {
            Some(tex) => {
                let uv = cover_uv(tex.size_vec2(), screen.size());
                painter.image(tex.id(), screen, uv, Color32::WHITE);
                let alpha = (self.settings.wallpaper_dim.clamp(0.0, 0.9) * 255.0) as u8;
                painter.rect_filled(screen, 0.0, Color32::from_black_alpha(alpha));
            }
            None => painter.rect_filled(screen, 0.0, Color32::from_rgb(18, 22, 34)),
        }
    }

    // --------------------------------------------------------------- tasks

    fn start_play(&mut self, ctx: &egui::Context) {
        let Some(inst) = self.selected.and_then(|i| self.instances.get(i)).cloned() else {
            return;
        };
        {
            let mut g = self.sh.lock().unwrap();
            if g.busy {
                return;
            }
            g.busy = true;
            g.progress = None;
            g.log.clear();
        }
        self.settings.last_instance = Some(inst.name.clone());
        let _ = self.settings.save(&self.dirs);

        let offline = self.use_offline.then(|| self.settings.offline_username.clone());
        let account = self.selected_account.clone();
        let client_id = self.accounts.client_id.clone();
        let (dirs, client, sh, ctx) =
            (self.dirs.clone(), self.client.clone(), self.sh.clone(), ctx.clone());

        self.rt.spawn(async move {
            let res = play(&dirs, &client, &sh, &ctx, inst, offline, account, client_id).await;
            {
                let mut g = sh.lock().unwrap();
                g.busy = false;
                g.running = false;
                g.progress = None;
                g.log.push(match res {
                    Ok(()) => "Game closed.".to_string(),
                    Err(e) => format!("Error: {e}"),
                });
            }
            ctx.request_repaint();
        });
    }

    fn start_login(&mut self, ctx: &egui::Context) {
        let typed = self.client_id_input.trim().to_string();
        let id = if typed.is_empty() {
            self.accounts.client_id.clone().unwrap_or_default()
        } else {
            typed
        };
        if id.is_empty() {
            set_status(&self.sh, "Enter your Azure application (client) ID first.");
            return;
        }
        {
            let mut g = self.sh.lock().unwrap();
            if g.logging_in {
                return;
            }
            g.logging_in = true;
            g.status = "Requesting a sign-in code...".into();
        }
        let (dirs, client, sh, ctx) =
            (self.dirs.clone(), self.client.clone(), self.sh.clone(), ctx.clone());
        self.rt.spawn(async move {
            let show = {
                let (sh, ctx) = (sh.clone(), ctx.clone());
                move |dc: &DeviceCode| {
                    sh.lock().unwrap().device_code = Some(dc.clone());
                    ctx.request_repaint();
                }
            };
            let res = accounts::login(&client, &dirs, &id, &show).await;
            {
                let mut g = sh.lock().unwrap();
                g.logging_in = false;
                g.device_code = None;
                g.accounts_dirty = true;
                g.status = match res {
                    Ok(a) => format!("Signed in as {}.", a.username),
                    Err(e) => format!("Sign-in failed: {e}"),
                };
            }
            ctx.request_repaint();
        });
    }

    // ----------------------------------------------------------------- UI

    fn ui_play(&mut self, ui: &mut egui::Ui, ctx: &egui::Context) {
        let (busy, running, progress) = {
            let g = self.sh.lock().unwrap();
            (g.busy, g.running, g.progress.clone())
        };

        if self.instances.is_empty() {
            ui.label("You have no instances yet. Create one on the Instances tab.");
            return;
        }

        ui.horizontal(|ui| {
            ui.label("Instance");
            let text = self
                .selected
                .and_then(|i| self.instances.get(i))
                .map(|i| format!("{} ({})", i.name, i.version))
                .unwrap_or_else(|| "none".into());
            egui::ComboBox::new("play_instance", "").selected_text(text).show_ui(ui, |ui| {
                for (i, inst) in self.instances.iter().enumerate() {
                    ui.selectable_value(
                        &mut self.selected,
                        Some(i),
                        format!("{} ({})", inst.name, inst.version),
                    );
                }
            });
        });

        ui.horizontal(|ui| {
            ui.checkbox(&mut self.use_offline, "Offline mode (testing only)");
            if self.use_offline {
                ui.text_edit_singleline(&mut self.settings.offline_username);
            } else if self.accounts.accounts.is_empty() {
                ui.label("No account yet. Sign in on the Accounts tab.");
            } else {
                let text = self.selected_account.clone().unwrap_or_else(|| "none".into());
                egui::ComboBox::new("play_account", "Account").selected_text(text).show_ui(ui, |ui| {
                    for a in &self.accounts.accounts {
                        ui.selectable_value(
                            &mut self.selected_account,
                            Some(a.username.clone()),
                            &a.username,
                        );
                    }
                });
            }
        });

        ui.add_space(6.0);
        let can_play = !busy
            && self.selected.is_some()
            && (self.use_offline || self.selected_account.is_some());
        let label = if running {
            "Running..."
        } else if busy {
            "Preparing..."
        } else {
            "Play"
        };
        let button = egui::Button::new(RichText::new(label).size(20.0))
            .min_size(egui::vec2(180.0, 42.0));
        if ui.add_enabled(can_play, button).clicked() {
            self.start_play(ctx);
        }

        if let Some((stage, done, total)) = progress {
            let frac = if total == 0 { 1.0 } else { done as f32 / total as f32 };
            ui.add(egui::ProgressBar::new(frac).text(format!("{stage}  {done}/{total}")));
        }

        ui.separator();
        let row_h = ui.text_style_height(&egui::TextStyle::Monospace);
        let g = self.sh.lock().unwrap();
        egui::ScrollArea::vertical()
            .auto_shrink([false, false])
            .stick_to_bottom(true)
            .show_rows(ui, row_h, g.log.len(), |ui, range| {
                for i in range {
                    ui.monospace(&g.log[i]);
                }
            });
    }

    fn ui_instances(&mut self, ui: &mut egui::Ui) {
        ui.heading("Instances");

        let versions: Vec<String> = {
            let g = self.sh.lock().unwrap();
            g.versions
                .iter()
                .filter(|v| {
                    v.kind == "release" || (self.settings.show_snapshots && v.kind == "snapshot")
                })
                .map(|v| v.id.clone())
                .collect()
        };
        if self.new_version.is_empty() {
            if let Some(first) = versions.first() {
                self.new_version = first.clone();
            }
        }

        let mut create = false;
        ui.horizontal(|ui| {
            ui.label("New:");
            ui.add(egui::TextEdit::singleline(&mut self.new_name).hint_text("name").desired_width(140.0));
            egui::ComboBox::new("new_version", "").selected_text(self.new_version.clone()).show_ui(
                ui,
                |ui| {
                    egui::ScrollArea::vertical().max_height(300.0).show(ui, |ui| {
                        for v in &versions {
                            ui.selectable_value(&mut self.new_version, v.clone(), v);
                        }
                    });
                },
            );
            if ui
                .add_enabled(
                    !self.new_name.trim().is_empty() && !self.new_version.is_empty(),
                    egui::Button::new("Create"),
                )
                .clicked()
            {
                create = true;
            }
            ui.checkbox(&mut self.settings.show_snapshots, "snapshots");
        });
        if create {
            match Instance::create(&self.dirs, self.new_name.trim(), &self.new_version) {
                Ok(inst) => {
                    set_status(&self.sh, format!("Created '{}'.", inst.name));
                    self.new_name.clear();
                    self.reload_instances();
                    self.selected = self.instances.iter().position(|i| i.name == inst.name);
                }
                Err(e) => set_status(&self.sh, format!("Could not create instance: {e}")),
            }
        }

        ui.separator();
        ui.horizontal_top(|ui| {
            ui.vertical(|ui| {
                ui.set_min_width(200.0);
                for (i, inst) in self.instances.iter().enumerate() {
                    let label = format!("{} ({})", inst.name, inst.version);
                    if ui.selectable_label(self.selected == Some(i), label).clicked() {
                        self.selected = Some(i);
                        self.confirm_delete = false;
                    }
                }
            });
            ui.separator();
            ui.vertical(|ui| self.ui_instance_editor(ui));
        });

        let status = self.sh.lock().unwrap().status.clone();
        if !status.is_empty() {
            ui.separator();
            ui.label(status);
        }
    }

    fn ui_instance_editor(&mut self, ui: &mut egui::Ui) {
        let Some(i) = self.selected.filter(|i| *i < self.instances.len()) else {
            ui.label("Select an instance.");
            return;
        };
        if self.args_for != Some(i) {
            self.args_buf = self.instances[i].jvm_args.join(" ");
            self.args_for = Some(i);
        }

        let max_mem = optimize::max_memory_mb();
        let mut save = false;
        let mut delete = false;
        {
            let inst = &mut self.instances[i];
            ui.label(RichText::new(format!("{}  -  Minecraft {}", inst.name, inst.version)).strong());
            ui.add_space(4.0);

            ui.horizontal(|ui| {
                ui.label("Memory");
                ui.add(
                    egui::Slider::new(&mut inst.memory_mb, 1024..=max_mem)
                        .suffix(" MB")
                        .step_by(256.0),
                );
                if ui.button("Auto").on_hover_text("Pick a size that suits this PC").clicked() {
                    inst.memory_mb = optimize::recommended_memory_mb();
                }
            });

            ui.horizontal(|ui| {
                ui.label("Optimisation");
                egui::ComboBox::new("profile", "")
                    .selected_text(inst.profile.label())
                    .show_ui(ui, |ui| {
                        for p in Profile::ALL {
                            ui.selectable_value(&mut inst.profile, p, p.label());
                        }
                    });
            });
            ui.label(match inst.profile {
                Profile::Off => "No extra flags; the JVM uses its own defaults.",
                Profile::Balanced => "G1 garbage collector with short pauses. Safe for everything.",
                Profile::Performance => "Tuned G1 flags that reduce stutter, best for modded play.",
            });

            ui.horizontal(|ui| {
                ui.label("Extra JVM args");
                ui.text_edit_singleline(&mut self.args_buf);
            });

            let mut java_text = inst
                .java_path
                .as_ref()
                .map(|p| p.display().to_string())
                .unwrap_or_default();
            ui.horizontal(|ui| {
                ui.label("Java override");
                let r = ui.add(
                    egui::TextEdit::singleline(&mut java_text)
                        .hint_text("empty = automatic (recommended)"),
                );
                if r.changed() {
                    inst.java_path =
                        if java_text.trim().is_empty() { None } else { Some(PathBuf::from(java_text.trim())) };
                }
            });

            ui.add_space(6.0);
            ui.horizontal(|ui| {
                if ui.button("Save").clicked() {
                    inst.jvm_args = self.args_buf.split_whitespace().map(String::from).collect();
                    save = true;
                }
                if self.confirm_delete {
                    ui.colored_label(Color32::LIGHT_RED, "This deletes worlds and mods too.");
                    if ui.button("Yes, delete").clicked() {
                        delete = true;
                    }
                    if ui.button("Cancel").clicked() {
                        self.confirm_delete = false;
                    }
                } else if ui.button("Delete...").clicked() {
                    self.confirm_delete = true;
                }
            });
        }

        if save {
            match self.instances[i].save(&self.dirs) {
                Ok(()) => set_status(&self.sh, "Saved."),
                Err(e) => set_status(&self.sh, format!("Could not save: {e}")),
            }
        }
        if delete {
            let name = self.instances[i].name.clone();
            match std::fs::remove_dir_all(self.dirs.instance_dir(&name)) {
                Ok(()) => set_status(&self.sh, format!("Deleted '{name}'.")),
                Err(e) => set_status(&self.sh, format!("Could not delete: {e}")),
            }
            self.confirm_delete = false;
            self.reload_instances();
        }
    }

    fn ui_accounts(&mut self, ui: &mut egui::Ui, ctx: &egui::Context) {
        ui.heading("Microsoft accounts");
        let (logging_in, code, status) = {
            let g = self.sh.lock().unwrap();
            (g.logging_in, g.device_code.clone(), g.status.clone())
        };

        let mut remove: Option<String> = None;
        if self.accounts.accounts.is_empty() {
            ui.label("No accounts saved yet.");
        }
        for a in &self.accounts.accounts {
            ui.horizontal(|ui| {
                ui.label(&a.username);
                if ui.small_button("Remove").clicked() {
                    remove = Some(a.username.clone());
                }
            });
        }
        if let Some(name) = remove {
            let mut store = AccountStore::load(&self.dirs).unwrap_or_default();
            if store.remove(&name) {
                let _ = store.save(&self.dirs);
            }
            self.reload_accounts();
        }

        ui.separator();
        ui.horizontal(|ui| {
            ui.label("Azure client ID");
            let hint = if self.accounts.client_id.is_some() {
                "saved from your last sign-in"
            } else {
                "paste your application (client) ID"
            };
            ui.add(
                egui::TextEdit::singleline(&mut self.client_id_input)
                    .hint_text(hint)
                    .desired_width(320.0),
            );
        });
        if ui.add_enabled(!logging_in, egui::Button::new("Sign in with Microsoft")).clicked() {
            self.start_login(ctx);
        }

        if let Some(dc) = code {
            ui.add_space(8.0);
            egui::Frame::group(ui.style()).show(ui, |ui| {
                ui.label("1. Open the Microsoft sign-in page:");
                if ui.link(&dc.verification_uri).clicked() {
                    ctx.open_url(egui::OpenUrl::new_tab(&dc.verification_uri));
                }
                ui.label("2. Enter this code:");
                ui.label(RichText::new(&dc.user_code).size(28.0).strong().monospace());
                ui.label("Waiting for you to finish...");
            });
        }
        if !status.is_empty() {
            ui.add_space(6.0);
            ui.label(status);
        }
    }

    fn ui_appearance(&mut self, ui: &mut egui::Ui, ctx: &egui::Context) {
        ui.heading("Wallpaper");
        ui.label(
            "Choose any image from your computer, or drop an image file onto this window. \
             Big images are shrunk and stored in java4u's own folder.",
        );
        ui.add_space(4.0);

        ui.horizontal(|ui| {
            if ui.button("Choose image...").clicked() {
                let picked = rfd::FileDialog::new()
                    .add_filter("Images", &["png", "jpg", "jpeg", "webp", "gif", "bmp"])
                    .pick_file();
                if let Some(path) = picked {
                    self.import_wallpaper_async(ctx, path);
                }
            }
            let has = self.settings.wallpaper.is_some();
            if ui.add_enabled(has, egui::Button::new("No wallpaper")).clicked() {
                self.settings.wallpaper = None;
                let _ = self.settings.save(&self.dirs);
            }
        });

        ui.add_space(6.0);
        ui.horizontal(|ui| {
            ui.label("Darkening");
            if ui.add(egui::Slider::new(&mut self.settings.wallpaper_dim, 0.0..=0.9)).changed() {
                self.dim_dirty = true;
            }
        });

        ui.separator();
        ui.label(format!("Your wallpapers ({})", self.wallpaper_list.len()));
        let mut use_name: Option<String> = None;
        let mut delete_name: Option<String> = None;
        egui::ScrollArea::vertical().auto_shrink([false, true]).show(ui, |ui| {
            for name in &self.wallpaper_list {
                ui.horizontal(|ui| {
                    let active = self.settings.wallpaper.as_deref() == Some(name.as_str());
                    let shown = name.trim_end_matches(".jpg");
                    ui.label(if active { format!("{shown}  (in use)") } else { shown.to_string() });
                    if ui.add_enabled(!active, egui::Button::new("Use")).clicked() {
                        use_name = Some(name.clone());
                    }
                    if ui.button("Delete").clicked() {
                        delete_name = Some(name.clone());
                    }
                });
            }
        });
        if let Some(name) = use_name {
            self.settings.wallpaper = Some(name);
            let _ = self.settings.save(&self.dirs);
        }
        if let Some(name) = delete_name {
            let _ = wallpaper::remove(&self.dirs, &name);
            if self.settings.wallpaper.as_deref() == Some(name.as_str()) {
                self.settings.wallpaper = None;
                let _ = self.settings.save(&self.dirs);
            }
            self.wallpaper_list = wallpaper::list(&self.dirs).unwrap_or_default();
        }

        let status = self.sh.lock().unwrap().status.clone();
        if !status.is_empty() {
            ui.separator();
            ui.label(status);
        }
    }
}

impl eframe::App for App {
    fn update(&mut self, ctx: &egui::Context, _frame: &mut eframe::Frame) {
        self.poll(ctx);
        self.paint_background(ctx);

        egui::TopBottomPanel::top("tabs").show(ctx, |ui| {
            ui.horizontal(|ui| {
                ui.heading("java4u");
                ui.separator();
                ui.selectable_value(&mut self.tab, Tab::Play, "Play");
                ui.selectable_value(&mut self.tab, Tab::Instances, "Instances");
                ui.selectable_value(&mut self.tab, Tab::Accounts, "Accounts");
                ui.selectable_value(&mut self.tab, Tab::Appearance, "Appearance");
            });
        });

        egui::CentralPanel::default().show(ctx, |ui| match self.tab {
            Tab::Play => self.ui_play(ui, ctx),
            Tab::Instances => self.ui_instances(ui),
            Tab::Accounts => self.ui_accounts(ui, ctx),
            Tab::Appearance => self.ui_appearance(ui, ctx),
        });

        // Save the darkening slider once the pointer is released.
        if self.dim_dirty && !ctx.input(|i| i.pointer.any_down()) {
            let _ = self.settings.save(&self.dirs);
            self.dim_dirty = false;
        }
    }
}

/// UV rectangle that makes the image cover the whole area (crop, no stretching).
fn cover_uv(image: egui::Vec2, area: egui::Vec2) -> egui::Rect {
    let image_aspect = image.x / image.y;
    let area_aspect = area.x / area.y;
    if area_aspect > image_aspect {
        let h = image_aspect / area_aspect;
        let y0 = (1.0 - h) / 2.0;
        egui::Rect::from_min_max(egui::pos2(0.0, y0), egui::pos2(1.0, y0 + h))
    } else {
        let w = area_aspect / image_aspect;
        let x0 = (1.0 - w) / 2.0;
        egui::Rect::from_min_max(egui::pos2(x0, 0.0), egui::pos2(x0 + w, 1.0))
    }
}

#[allow(clippy::too_many_arguments)]
async fn play(
    dirs: &Dirs,
    client: &java4u_core::reqwest::Client,
    sh: &Sh,
    ctx: &egui::Context,
    inst: Instance,
    offline: Option<String>,
    account: Option<String>,
    client_id: Option<String>,
) -> java4u_core::Result<()> {
    let progress = {
        let (sh, ctx) = (sh.clone(), ctx.clone());
        move |stage: &str, done: usize, total: usize| {
            sh.lock().unwrap().progress = Some((stage.to_string(), done, total));
            ctx.request_repaint();
        }
    };

    let version = install_version(client, dirs, &inst.version, &progress).await?;
    let java = resolve_java(client, dirs, &version, inst.java_path.as_deref(), &progress).await?;
    log(sh, format!("Using Java: {}", java.display()));

    let acct = match offline {
        Some(name) => Account::offline(&name)?,
        None => {
            accounts::account_for_launch(client, dirs, account.as_deref(), client_id.as_deref())
                .await?
        }
    };

    let opts = LaunchOptions {
        java,
        game_dir: inst.game_dir(dirs),
        account: acct,
        memory_mb: inst.memory_mb,
        extra_jvm_args: inst.effective_jvm_args(),
    };
    let cmd = build_command(dirs, &version, &opts)?;

    {
        let mut g = sh.lock().unwrap();
        g.progress = None;
        g.running = true;
    }
    log(sh, "Launching Minecraft...");
    ctx.request_repaint();

    let (sh2, ctx2) = (sh.clone(), ctx.clone());
    tokio::task::spawn_blocking(move || run_logged(cmd, sh2, ctx2))
        .await
        .map_err(|e| Error::Other(e.to_string()))?
}

/// Run the game, streaming its output into the log until it exits.
fn run_logged(mut cmd: std::process::Command, sh: Sh, ctx: egui::Context) -> java4u_core::Result<()> {
    cmd.stdout(Stdio::piped()).stderr(Stdio::piped());
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        cmd.creation_flags(0x0800_0000); // CREATE_NO_WINDOW
    }
    let mut child = cmd
        .spawn()
        .map_err(|e| Error::Other(format!("failed to start Java: {e}")))?;

    let mut readers = Vec::new();
    if let Some(out) = child.stdout.take() {
        let (sh, ctx) = (sh.clone(), ctx.clone());
        readers.push(std::thread::spawn(move || {
            for line in BufReader::new(out).lines().map_while(|l| l.ok()) {
                log(&sh, line);
                ctx.request_repaint();
            }
        }));
    }
    if let Some(err) = child.stderr.take() {
        let (sh, ctx) = (sh.clone(), ctx.clone());
        readers.push(std::thread::spawn(move || {
            for line in BufReader::new(err).lines().map_while(|l| l.ok()) {
                log(&sh, line);
                ctx.request_repaint();
            }
        }));
    }

    let status = child.wait()?;
    for r in readers {
        let _ = r.join();
    }
    if !status.success() {
        return Err(Error::Other(format!("Minecraft exited with {status}")));
    }
    Ok(())
}
