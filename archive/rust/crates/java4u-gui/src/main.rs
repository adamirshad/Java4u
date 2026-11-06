#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod app;

fn main() -> Result<(), eframe::Error> {
    let options = eframe::NativeOptions {
        viewport: eframe::egui::ViewportBuilder::default()
            .with_title("java4u")
            .with_inner_size([1000.0, 640.0])
            .with_min_inner_size([820.0, 520.0]),
        ..Default::default()
    };
    eframe::run_native(
        "java4u",
        options,
        Box::new(|cc| Ok(Box::new(app::App::new(cc)))),
    )
}
