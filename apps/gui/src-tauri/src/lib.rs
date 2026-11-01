mod ensure;
mod session;

pub fn run() {
    tauri::Builder::default()
        .manage(session::Sessions::default())
        .invoke_handler(tauri::generate_handler![
            session::session_open,
            session::session_send,
            session::session_poll,
            session::session_close,
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
