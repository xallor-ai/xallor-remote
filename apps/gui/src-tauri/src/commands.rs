use tauri::{AppHandle, Emitter};

#[tauri::command]
pub fn status() -> Result<serde_json::Value, String> {
    crate::ipc::rpc("status", serde_json::json!({}))
}

#[tauri::command]
pub fn grant_show() -> Result<String, String> {
    grant_field("grant.show")
}

#[tauri::command]
pub fn grant_issue() -> Result<String, String> {
    grant_field("grant.issue")
}

#[tauri::command]
pub fn grant_rotate() -> Result<String, String> {
    grant_field("grant.rotate")
}

fn grant_field(method: &str) -> Result<String, String> {
    let v = crate::ipc::rpc(method, serde_json::json!({}))?;
    Ok(v.get("grant").and_then(|x| x.as_str()).unwrap_or("").to_string())
}

#[tauri::command]
pub fn inbound_set(enabled: bool) -> Result<(), String> {
    crate::ipc::rpc("inbound.set", serde_json::json!({"enabled": enabled}))?;
    Ok(())
}

#[tauri::command]
pub fn peer_list() -> Result<Vec<String>, String> {
    let v = crate::ipc::rpc("peer.list", serde_json::json!({}))?;
    Ok(v.get("peers")
        .and_then(|x| x.as_array())
        .map(|a| {
            a.iter()
                .filter_map(|x| x.as_str().map(|s| s.to_string()))
                .collect()
        })
        .unwrap_or_default())
}

#[tauri::command]
pub fn peer_add(device_id: String, grant: String) -> Result<(), String> {
    crate::ipc::rpc(
        "peer.add",
        serde_json::json!({"device_id": device_id, "grant": grant}),
    )?;
    Ok(())
}

#[tauri::command]
pub fn peer_remove(device_id: String) -> Result<(), String> {
    crate::ipc::rpc("peer.remove", serde_json::json!({"device_id": device_id}))?;
    Ok(())
}

#[tauri::command]
pub fn config_get() -> Result<serde_json::Value, String> {
    crate::ipc::rpc("config.get", serde_json::json!({}))
}

#[tauri::command]
pub fn config_set_workspace(workspace: String) -> Result<(), String> {
    if workspace.trim().is_empty() {
        return Err("请填写 workspace 路径。".into());
    }
    crate::ipc::rpc(
        "config.set",
        serde_json::json!({"workspace": workspace.trim()}),
    )?;
    Ok(())
}

#[tauri::command]
pub fn exec_cmd(app: AppHandle, command: String, device_id: String) -> Result<(), String> {
    crate::ipc::exec_stream(&command, &device_id, |s| {
        let _ = app.emit("exec-out", s);
    })
}
