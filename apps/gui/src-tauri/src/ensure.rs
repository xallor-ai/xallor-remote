use std::env;
use std::path::{Path, PathBuf};

const INSTALL_HINT: &str =
    "找不到 xallor-remote。请安装带 Runtime 的 xallor-remote-mcp，或设置 XALLOR_REMOTE_BIN。";

fn exe_name() -> &'static str {
    if cfg!(windows) {
        "xallor-remote.exe"
    } else {
        "xallor-remote"
    }
}

fn vendor_platform_key() -> &'static str {
    // Keep in sync with packages/mcp/src/ensure.ts vendorPlatformKey().
    #[cfg(all(target_os = "windows", any(target_arch = "x86_64", target_arch = "aarch64")))]
    {
        "win32-x64"
    }
    #[cfg(all(target_os = "linux", target_arch = "x86_64"))]
    {
        "linux-x64"
    }
    #[cfg(all(target_os = "linux", target_arch = "aarch64"))]
    {
        "linux-arm64"
    }
    #[cfg(all(target_os = "macos", target_arch = "aarch64"))]
    {
        "darwin-arm64"
    }
    #[cfg(all(target_os = "macos", target_arch = "x86_64"))]
    {
        "darwin-x64"
    }
    #[cfg(not(any(
        all(target_os = "windows", any(target_arch = "x86_64", target_arch = "aarch64")),
        all(target_os = "linux", target_arch = "x86_64"),
        all(target_os = "linux", target_arch = "aarch64"),
        all(target_os = "macos", target_arch = "aarch64"),
        all(target_os = "macos", target_arch = "x86_64"),
    )))]
    {
        "unknown"
    }
}

fn data_dir() -> PathBuf {
    if let Ok(dir) = env::var("XALLOR_REMOTE_DATA_DIR") {
        if !dir.is_empty() {
            return PathBuf::from(dir);
        }
    }
    if cfg!(windows) {
        if let Ok(base) = env::var("APPDATA") {
            return PathBuf::from(base).join("XallorRemote");
        }
        let home = env::var("USERPROFILE").unwrap_or_default();
        return PathBuf::from(home)
            .join("AppData")
            .join("Roaming")
            .join("XallorRemote");
    }
    if let Ok(xdg) = env::var("XDG_CONFIG_HOME") {
        return PathBuf::from(xdg).join("xallor-remote");
    }
    let home = env::var("HOME").unwrap_or_default();
    PathBuf::from(home).join(".config").join("xallor-remote")
}

fn vendor_bin(prefix: &Path) -> Option<PathBuf> {
    let cand = prefix
        .join("node_modules")
        .join("xallor-remote-mcp")
        .join("vendor")
        .join(vendor_platform_key())
        .join(exe_name());
    cand.is_file().then_some(cand)
}

/// npm global shims are .cmd/.ps1; Tauri needs the real vendor binary.
fn look_npm_vendor() -> Option<PathBuf> {
    let mut prefixes = Vec::new();
    if cfg!(windows) {
        if let Ok(appdata) = env::var("APPDATA") {
            prefixes.push(PathBuf::from(appdata).join("npm"));
        }
    }
    if let Some(path) = env::var_os("PATH") {
        for dir in env::split_paths(&path) {
            if dir.join("xallor-remote.cmd").is_file()
                || dir.join("xallor-remote").is_file()
                || dir.join(exe_name()).is_file()
            {
                prefixes.push(dir);
            }
        }
    }
    prefixes.into_iter().find_map(|p| vendor_bin(&p))
}

fn look_in_path() -> Option<PathBuf> {
    let name = exe_name();
    let path = env::var_os("PATH")?;
    for dir in env::split_paths(&path) {
        let cand = dir.join(name);
        if cand.is_file() {
            return Some(cand);
        }
    }
    None
}

pub fn find_bin() -> Result<PathBuf, String> {
    if let Ok(p) = env::var("XALLOR_REMOTE_BIN") {
        if !p.is_empty() {
            return Ok(PathBuf::from(p));
        }
    }
    // Prefer npm vendor before PATH: Windows PATH often only has unusable .cmd shims.
    if let Some(p) = look_npm_vendor() {
        return Ok(p);
    }
    if let Some(p) = look_in_path() {
        return Ok(p);
    }
    let bundled = data_dir().join("bin").join(exe_name());
    if bundled.is_file() {
        return Ok(bundled);
    }
    Err(INSTALL_HINT.into())
}
