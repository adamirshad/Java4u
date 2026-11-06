//! Performance helpers: sensible memory sizing and JVM flag presets.

use std::fmt;
use std::str::FromStr;

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Profile {
    /// Add no flags; the JVM picks its own defaults.
    Off,
    /// G1 garbage collector with sane pause targets. Safe everywhere.
    #[default]
    Balanced,
    /// Tuned G1 flags that smooth out stutter on heavier modded setups.
    Performance,
}

impl Profile {
    pub const ALL: [Profile; 3] = [Profile::Off, Profile::Balanced, Profile::Performance];

    pub fn label(self) -> &'static str {
        match self {
            Profile::Off => "Off",
            Profile::Balanced => "Balanced",
            Profile::Performance => "Performance",
        }
    }
}

impl fmt::Display for Profile {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.label().to_lowercase().as_str())
    }
}

impl FromStr for Profile {
    type Err = String;
    fn from_str(s: &str) -> Result<Self, String> {
        match s.to_ascii_lowercase().as_str() {
            "off" | "none" => Ok(Profile::Off),
            "balanced" => Ok(Profile::Balanced),
            "performance" | "perf" => Ok(Profile::Performance),
            other => Err(format!("unknown profile '{other}' (use off, balanced or performance)")),
        }
    }
}

pub fn total_memory_mb() -> u64 {
    let mut sys = sysinfo::System::new();
    sys.refresh_memory();
    sys.total_memory() / 1024 / 1024
}

/// A good default heap size for this machine: never more than half of the RAM.
pub fn recommended_memory_mb() -> u32 {
    let total = total_memory_mb();
    if total == 0 {
        return 2048;
    }
    let tier: u32 = match total {
        0..=4096 => 2048,
        4097..=8192 => 3072,
        8193..=16384 => 4096,
        _ => 6144,
    };
    tier.min((total / 2) as u32).max(1024)
}

/// Upper bound offered in memory sliders: 75% of RAM.
pub fn max_memory_mb() -> u32 {
    ((total_memory_mb() * 3 / 4) as u32).max(2048)
}

const BALANCED: &[&str] = &[
    "-XX:+UseG1GC",
    "-XX:+ParallelRefProcEnabled",
    "-XX:MaxGCPauseMillis=200",
];

const PERFORMANCE_EXTRA: &[&str] = &[
    "-XX:+UnlockExperimentalVMOptions",
    "-XX:+DisableExplicitGC",
    "-XX:G1NewSizePercent=30",
    "-XX:G1MaxNewSizePercent=40",
    "-XX:G1HeapRegionSize=8M",
    "-XX:G1ReservePercent=20",
    "-XX:G1HeapWastePercent=5",
    "-XX:G1MixedGCCountTarget=4",
    "-XX:InitiatingHeapOccupancyPercent=15",
    "-XX:G1MixedGCLiveThresholdPercent=90",
    "-XX:G1RSetUpdatingPauseTimePercent=5",
    "-XX:SurvivorRatio=32",
    "-XX:+PerfDisableSharedMem",
    "-XX:MaxTenuringThreshold=1",
];

/// JVM flags for a profile. Respects the user's own -Xms and GC choices.
pub fn jvm_flags(profile: Profile, memory_mb: u32, user_args: &[String]) -> Vec<String> {
    let mut flags = Vec::new();
    if profile == Profile::Off {
        return flags;
    }
    if !user_args.iter().any(|a| a.starts_with("-Xms")) {
        flags.push(format!("-Xms{}M", memory_mb.min(1024)));
    }
    if user_args.iter().any(|a| a.starts_with("-XX:+Use") && a.ends_with("GC")) {
        return flags;
    }
    flags.extend(BALANCED.iter().map(|s| s.to_string()));
    if profile == Profile::Performance {
        flags.extend(PERFORMANCE_EXTRA.iter().map(|s| s.to_string()));
    }
    flags
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn off_adds_nothing() {
        assert!(jvm_flags(Profile::Off, 4096, &[]).is_empty());
    }

    #[test]
    fn user_gc_choice_wins() {
        let user = vec!["-XX:+UseZGC".to_string(), "-Xms2G".to_string()];
        assert!(jvm_flags(Profile::Performance, 4096, &user).is_empty());
    }

    #[test]
    fn initial_heap_is_capped() {
        let f = jvm_flags(Profile::Balanced, 512, &[]);
        assert_eq!(f[0], "-Xms512M");
        let f = jvm_flags(Profile::Balanced, 8192, &[]);
        assert_eq!(f[0], "-Xms1024M");
    }

    #[test]
    fn experimental_unlock_precedes_experimental_flags() {
        let f = jvm_flags(Profile::Performance, 4096, &[]);
        let unlock = f.iter().position(|a| a == "-XX:+UnlockExperimentalVMOptions").unwrap();
        let g1 = f.iter().position(|a| a.starts_with("-XX:G1NewSizePercent")).unwrap();
        assert!(unlock < g1);
    }

    #[test]
    fn recommended_memory_is_sane() {
        let m = recommended_memory_mb();
        assert!((1024..=6144).contains(&m));
    }

    #[test]
    fn parses_profiles() {
        assert_eq!("perf".parse::<Profile>().unwrap(), Profile::Performance);
        assert!("nope".parse::<Profile>().is_err());
    }
}
