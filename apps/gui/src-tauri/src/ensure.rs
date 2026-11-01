use std::env;
use std::path::PathBuf;

fn exe_name() -> &'static str {
    if cfg!(windows) {
        "xallor-remote.exe"
    } else {
        "xallor-remote"
    }
}

fn data_dir() -> PathBuf {
    if let Ok(dir) = env::var("XALLOR_REMOTE_DATA_DIR") {
        if !dir.is_empty() { return PathBuf::from(dir); }
    }
    if cfg!(windows) {
        if let Ok(base) = env::var("APPDATA") {
            return PathBuf::from(base).join("XallorRemote");
        }
        let home = env::var("USERPROFILE").unwrap_or_default();
        return PathBuf::from(home).join("AppData").join("Roaming").join("XallorRemote");
    }
    if let Ok(xdg) = env::var("XDG_CONFIG_HOME") {
        return PathBuf::from(xdg).join("xallor-remote");
    }
    let home = env::var("HOME").unwrap_or_default();
    PathBuf::from(home).join(".config").join("xallor-remote")
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
    if let Some(p) = look_in_path() {
        return Ok(p);
    }
    let bundled = data_dir().join("bin").join(exe_name());
    if bundled.is_file() {
        return Ok(bundled);
    }
    Err("找不到 xallor-remote。请先安装后再打开。".into())
}
