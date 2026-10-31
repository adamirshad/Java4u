@echo off
where cargo >nul 2>nul
if errorlevel 1 (
  echo Rust is not installed. Get it from https://rustup.rs and run this again.
  exit /b 1
)
cargo build --release --workspace || exit /b 1
echo.
echo Built:
echo   target\release\java4u.exe       (command line)
echo   target\release\java4u-gui.exe   (desktop app)
cargo test --workspace
