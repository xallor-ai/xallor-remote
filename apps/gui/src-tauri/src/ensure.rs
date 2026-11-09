use serde::Deserialize;
use std::env;
use std::fs;
use std::path::{Path, PathBuf};

/// Same contract as packages/mcp INSTALL_HINT and docs/stack.md lookup order.
pub const INSTALL_HINT: &str = "找不到 xallor-remote。请安装带 Runtime 的 xallor-remote-mcp，或运行 xallor-remote runtime install --bundle <解压目录>，或设置 XALLOR_REMOTE_BIN。GUI 不会静默联网下载。";

fn exe_names() -> &'static [&'static str] {
    if cfg!(windows) {
        &["xallor-remote.exe", "xallor-remote"]
    } else {
        &["xallor-remote"]
    }
}

fn vendor_platform_key() -> &'static str {
    // Keep in sync with packages/mcp/src/ensure.ts vendorPlatformKey().
    #[cfg(all(target_os = "windows", target_arch = "x86_64"))]
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
        all(target_os = "windows", target_arch = "x86_64"),
        all(target_os = "linux", target_arch = "x86_64"),
        all(target_os = "linux", target_arch = "aarch64"),
        all(target_os = "macos", target_arch = "aarch64"),
        all(target_os = "macos", target_arch = "x86_64"),
    )))]
    {
        "unknown"
    }
}

pub fn data_dir() -> PathBuf {
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

fn first_named_file(dir: &Path) -> Option<PathBuf> {
    for name in exe_names() {
        let cand = dir.join(name);
        if cand.is_file() {
            return Some(cand);
        }
    }
    None
}

/// npm global layout: <prefix>/node_modules/xallor-remote-mcp/vendor/<platform>/
fn npm_vendor_under_prefix(prefix: &Path) -> Option<PathBuf> {
    first_named_file(
        &prefix
            .join("node_modules")
            .join("xallor-remote-mcp")
            .join("vendor")
            .join(vendor_platform_key()),
    )
}

fn look_npm_vendor() -> Option<PathBuf> {
    let mut prefixes = Vec::new();
    if cfg!(windows) {
        if let Ok(appdata) = env::var("APPDATA") {
            prefixes.push(PathBuf::from(appdata).join("npm"));
        }
    } else {
        if let Ok(home) = env::var("HOME") {
            let home = PathBuf::from(home);
            prefixes.push(home.join(".npm-global"));
            prefixes.push(home.join(".local").join("lib"));
        }
    }
    if let Some(path) = env::var_os("PATH") {
        for dir in env::split_paths(&path) {
            // Global npm shims live in <prefix>; vendor lives under <prefix>/node_modules/...
            if dir.join("xallor-remote.cmd").is_file()
                || dir.join("xallor-remote").is_file()
                || dir.join("xallor-remote.exe").is_file()
            {
                prefixes.push(dir.clone());
            }
            // Also tolerate PATH entries that already point at node_modules parents.
            if dir.ends_with("npm") {
                prefixes.push(dir);
            }
        }
    }
    for prefix in prefixes {
        if let Some(hit) = npm_vendor_under_prefix(&prefix) {
            return Some(hit);
        }
    }
    None
}

fn look_in_path() -> Option<PathBuf> {
    let path = env::var_os("PATH")?;
    for dir in env::split_paths(&path) {
        if let Some(hit) = first_named_file(&dir) {
            // Prefer real binaries; ignore npm cmd/ps1 shims that cannot be spawned by Tauri.
            let name = hit.file_name()?.to_string_lossy();
            if name.ends_with(".cmd") || name.ends_with(".ps1") || name.ends_with(".bat") {
                continue;
            }
            return Some(hit);
        }
    }
    None
}

#[derive(Deserialize)]
struct ManagedState {
    pending: Option<bool>,
    current: Option<ManagedCurrent>,
}

#[derive(Deserialize)]
struct ManagedCurrent {
    schema: Option<u32>,
    artifact: Option<ManagedArtifact>,
}

#[derive(Deserialize)]
struct ManagedArtifact {
    filename: Option<String>,
    sha256: Option<String>,
    size: Option<u64>,
}

/// Prefer an already-installed managed Runtime (dataDir/runtime/versions/<sha>/...).
/// Integrity: size + sha256 from state.json. Provenance attestation stays MCP/CLI's job.
fn look_managed(data: &Path) -> Option<PathBuf> {
    let raw = fs::read_to_string(data.join("runtime").join("state.json")).ok()?;
    let state: ManagedState = serde_json::from_str(&raw).ok()?;
    if state.pending == Some(true) {
        return None;
    }
    let current = state.current?;
    if current.schema != Some(1) {
        return None;
    }
    let artifact = current.artifact?;
    let filename = artifact.filename?;
    let sha = artifact.sha256?;
    let size = artifact.size?;
    if !exe_names().iter().any(|n| *n == filename.as_str()) {
        return None;
    }
    if sha.len() != 64 || !sha.chars().all(|c| c.is_ascii_hexdigit()) {
        return None;
    }
    let bin = data
        .join("runtime")
        .join("versions")
        .join(&sha)
        .join(&filename);
    let meta = fs::metadata(&bin).ok()?;
    if !meta.is_file() || meta.len() != size {
        return None;
    }
    let bytes = fs::read(&bin).ok()?;
    let digest = sha256_hex(&bytes);
    if digest != sha.to_ascii_lowercase() {
        return None;
    }
    Some(bin)
}

fn sha256_hex(bytes: &[u8]) -> String {
    use sha2::{Digest, Sha256};
    let mut hasher = Sha256::new();
    hasher.update(bytes);
    hasher
        .finalize()
        .iter()
        .map(|b| format!("{b:02x}"))
        .collect()
}

/// docs/stack.md: XALLOR_REMOTE_BIN → managed install → npm vendor → PATH → dataDir/bin
pub fn find_bin() -> Result<PathBuf, String> {
    if let Ok(p) = env::var("XALLOR_REMOTE_BIN") {
        if !p.is_empty() {
            let path = PathBuf::from(&p);
            if path.is_file() {
                return Ok(path);
            }
            return Err(format!(
                "XALLOR_REMOTE_BIN 指向的文件不存在：{p}。{INSTALL_HINT}"
            ));
        }
    }
    let data = data_dir();
    if let Some(p) = look_managed(&data) {
        return Ok(p);
    }
    if let Some(p) = look_npm_vendor() {
        return Ok(p);
    }
    if let Some(p) = look_in_path() {
        return Ok(p);
    }
    if let Some(p) = first_named_file(&data.join("bin")) {
        return Ok(p);
    }
    Err(INSTALL_HINT.into())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;

    #[test]
    fn vendor_key_is_stable_on_this_host() {
        let key = vendor_platform_key();
        assert!(
            key == "win32-x64"
                || key == "linux-x64"
                || key == "linux-arm64"
                || key == "darwin-arm64"
                || key == "darwin-x64",
            "unexpected vendor key {key}"
        );
    }

    #[test]
    fn npm_vendor_under_prefix_finds_exe() {
        let root = std::env::temp_dir().join(format!(
            "xallor-gui-ensure-{}",
            std::process::id()
        ));
        let _ = fs::remove_dir_all(&root);
        let vendor = root
            .join("node_modules")
            .join("xallor-remote-mcp")
            .join("vendor")
            .join(vendor_platform_key());
        fs::create_dir_all(&vendor).unwrap();
        let name = exe_names()[0];
        let target = vendor.join(name);
        fs::write(&target, b"fake-bin").unwrap();
        let hit = npm_vendor_under_prefix(&root).expect("vendor hit");
        assert_eq!(hit, target);
        let _ = fs::remove_dir_all(&root);
    }

    #[test]
    fn managed_rejects_bad_hash() {
        let root = std::env::temp_dir().join(format!(
            "xallor-gui-managed-{}",
            std::process::id()
        ));
        let _ = fs::remove_dir_all(&root);
        let name = exe_names()[0];
        let sha = "ab".repeat(32);
        let ver = root.join("runtime").join("versions").join(&sha);
        fs::create_dir_all(&ver).unwrap();
        let bin = ver.join(name);
        fs::write(&bin, b"not-matching-hash").unwrap();
        let state = root.join("runtime").join("state.json");
        let mut f = fs::File::create(&state).unwrap();
        write!(
            f,
            r#"{{"current":{{"schema":1,"artifact":{{"filename":"{name}","sha256":"{sha}","size":{size}}}}}}}"#,
            name = name,
            sha = sha,
            size = fs::metadata(&bin).unwrap().len()
        )
        .unwrap();
        assert!(look_managed(&root).is_none());
        let _ = fs::remove_dir_all(&root);
    }

    #[test]
    fn managed_accepts_matching_hash() {
        let root = std::env::temp_dir().join(format!(
            "xallor-gui-managed-ok-{}",
            std::process::id()
        ));
        let _ = fs::remove_dir_all(&root);
        let name = exe_names()[0];
        let payload = b"managed-runtime-bytes";
        let sha = sha256_hex(payload);
        let ver = root.join("runtime").join("versions").join(&sha);
        fs::create_dir_all(&ver).unwrap();
        let bin = ver.join(name);
        fs::write(&bin, payload).unwrap();
        let state = root.join("runtime").join("state.json");
        let mut f = fs::File::create(&state).unwrap();
        write!(
            f,
            r#"{{"current":{{"schema":1,"artifact":{{"filename":"{name}","sha256":"{sha}","size":{size}}}}}}}"#,
            name = name,
            sha = sha,
            size = payload.len()
        )
        .unwrap();
        let hit = look_managed(&root).expect("managed hit");
        assert_eq!(hit, bin);
        let _ = fs::remove_dir_all(&root);
    }
}
