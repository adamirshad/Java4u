//! User-chosen background images. Imported images are validated, shrunk to at most
//! 2560x1440 and stored as JPEG in java4u's own folder, so they load fast and never
//! depend on the original file staying where it was.

use std::io::BufWriter;
use std::path::{Path, PathBuf};

use image::{codecs::jpeg::JpegEncoder, imageops::FilterType, ExtendedColorType, ImageEncoder};

use crate::error::{Error, Result};
use crate::paths::Dirs;

const MAX_SOURCE_BYTES: u64 = 64 * 1024 * 1024;
const IMPORT_MAX_W: u32 = 2560;
const IMPORT_MAX_H: u32 = 1440;
const JPEG_QUALITY: u8 = 90;

fn img_err(e: image::ImageError) -> Error {
    Error::Other(format!("image error: {e}"))
}

fn valid_name(name: &str) -> bool {
    !name.is_empty()
        && !name.starts_with('.')
        && !name.contains(['/', '\\'])
        && !name.contains("..")
        && name.ends_with(".jpg")
}

/// Names of all imported wallpapers, sorted.
pub fn list(dirs: &Dirs) -> Result<Vec<String>> {
    let mut out = Vec::new();
    let Ok(rd) = std::fs::read_dir(dirs.wallpapers_dir()) else {
        return Ok(out);
    };
    for entry in rd.flatten() {
        if let Some(name) = entry.file_name().to_str() {
            if valid_name(name) {
                out.push(name.to_string());
            }
        }
    }
    out.sort();
    Ok(out)
}

/// Full path of a stored wallpaper; rejects names that could point outside the folder.
pub fn path_of(dirs: &Dirs, name: &str) -> Result<PathBuf> {
    if !valid_name(name) {
        return Err(Error::Other("invalid wallpaper name".into()));
    }
    Ok(dirs.wallpapers_dir().join(name))
}

/// Import any png/jpg/webp/gif/bmp image. Returns the stored file name.
pub fn import(dirs: &Dirs, src: &Path) -> Result<String> {
    let meta = std::fs::metadata(src)?;
    if !meta.is_file() {
        return Err(Error::Other("not a file".into()));
    }
    if meta.len() > MAX_SOURCE_BYTES {
        return Err(Error::Other("image is larger than 64 MB".into()));
    }

    let img = image::ImageReader::open(src)?
        .with_guessed_format()?
        .decode()
        .map_err(img_err)?;
    let img = if img.width() > IMPORT_MAX_W || img.height() > IMPORT_MAX_H {
        img.resize(IMPORT_MAX_W, IMPORT_MAX_H, FilterType::Lanczos3)
    } else {
        img
    };
    let rgb = img.to_rgb8();

    let stem: String = src
        .file_stem()
        .and_then(|s| s.to_str())
        .unwrap_or("wallpaper")
        .chars()
        .map(|c| if c.is_ascii_alphanumeric() || c == '-' || c == '_' { c } else { '_' })
        .take(40)
        .collect();
    let stem = if stem.is_empty() { "wallpaper".to_string() } else { stem };

    std::fs::create_dir_all(dirs.wallpapers_dir())?;
    let mut n = 0;
    let (name, dest) = loop {
        let name = if n == 0 { format!("{stem}.jpg") } else { format!("{stem}-{n}.jpg") };
        let dest = dirs.wallpapers_dir().join(&name);
        if !dest.exists() {
            break (name, dest);
        }
        n += 1;
    };

    let file = std::fs::File::create(&dest)?;
    let writer = BufWriter::new(file);
    JpegEncoder::new_with_quality(writer, JPEG_QUALITY)
        .write_image(rgb.as_raw(), rgb.width(), rgb.height(), ExtendedColorType::Rgb8)
        .map_err(img_err)?;
    Ok(name)
}

pub fn remove(dirs: &Dirs, name: &str) -> Result<()> {
    std::fs::remove_file(path_of(dirs, name)?)?;
    Ok(())
}

/// Decode a stored wallpaper to RGBA, shrinking so no side exceeds `max_side`
/// (GPU texture limits). Returns (width, height, pixels).
pub fn load_rgba(path: &Path, max_side: u32) -> Result<(u32, u32, Vec<u8>)> {
    let img = image::ImageReader::open(path)?
        .with_guessed_format()?
        .decode()
        .map_err(img_err)?;
    let max_side = max_side.max(512);
    let img = if img.width() > max_side || img.height() > max_side {
        img.resize(max_side, max_side, FilterType::Lanczos3)
    } else {
        img
    };
    let rgba = img.to_rgba8();
    Ok((rgba.width(), rgba.height(), rgba.into_raw()))
}

#[cfg(test)]
mod tests {
    use super::valid_name;

    #[test]
    fn name_validation() {
        assert!(valid_name("sunset.jpg"));
        assert!(!valid_name("../x.jpg"));
        assert!(!valid_name("a/b.jpg"));
        assert!(!valid_name(".hidden.jpg"));
        assert!(!valid_name("x.png"));
    }
}
