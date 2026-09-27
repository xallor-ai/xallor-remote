mod commands;
mod ensure;
mod ipc;

pub fn run() {
    tauri::Builder::default()
        .invoke_handler(tauri::generate_handler![
            commands::status,
            commands::grant_show,
            commands::grant_issue,
            commands::grant_rotate,
            commands::inbound_set,
            commands::peer_list,
            commands::peer_add,
            commands::peer_remove,
            commands::config_get,
            commands::config_set_workspace,
            commands::exec_cmd,
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
