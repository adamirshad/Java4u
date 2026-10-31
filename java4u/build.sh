#!/usr/bin/env sh
# Build java4u (CLI + desktop app) for this machine.
set -e
if ! command -v cargo >/dev/null 2>&1; then
  echo "Rust is not installed. Get it from https://rustup.rs and run this again."
  exit 1
fi
cargo build --release --workspace
echo
echo "Built:"
echo "  ./target/release/java4u       (command line)"
echo "  ./target/release/java4u-gui   (desktop app)"
cargo test --workspace
