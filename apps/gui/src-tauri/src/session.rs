use serde::Serialize;
use serde_json::Value;
use std::{collections::HashMap, process::Stdio, sync::{Arc, Mutex, atomic::{AtomicU64, Ordering}}, time::{Duration, Instant}};
use tokio::{io::{AsyncBufReadExt, AsyncReadExt, AsyncWriteExt, BufReader}, process::{Child, ChildStdin, Command}, sync::Mutex as AsyncMutex};

const MAX_FRAME: usize = 2 * 1024 * 1024;
static NEXT: AtomicU64 = AtomicU64::new(1);

#[derive(Default)]
pub struct Sessions(Mutex<HashMap<String, Session>>);
struct Session {
    input: Arc<AsyncMutex<ChildStdin>>,
    queue: Arc<Mutex<Queue>>,
    reader: tokio::task::JoinHandle<()>,
}
impl Drop for Session {
    fn drop(&mut self) { self.reader.abort(); }
}
struct Queue { frames: Vec<Value>, bytes: usize, error: Option<String>, touched: Instant }
#[derive(Serialize)]
pub struct Batch { frames: Vec<Value>, error: Option<String> }

// The Go child owns profile resolution and exactly one IPC connection. Dropping
// this session kills only that child, which closes IPC and cancels owned tasks.
#[tauri::command]
pub async fn session_open(state: tauri::State<'_, Sessions>) -> Result<String, String> {
    let mut sessions = state.0.lock().map_err(|_| "连接状态不可用。")?;
    if sessions.len() >= 8 { return Err("打开的连接过多。".into()); }
    let session = launch()?;
    let id = NEXT.fetch_add(1, Ordering::Relaxed).to_string();
    sessions.insert(id.clone(), session);
    Ok(id)
}

fn launch() -> Result<Session, String> {
    let mut command = Command::new(crate::ensure::find_bin()?);
    command.args(["--require-profile-format", include_str!("../../../../internal/buildinfo/profile_format.txt").trim(), "ipc-bridge"])
        .stdin(Stdio::piped()).stdout(Stdio::piped()).stderr(Stdio::null()).kill_on_drop(true);
    #[cfg(windows)]
    command.creation_flags(0x0800_0000);
    let mut child = command.spawn().map_err(|_| {
        format!("无法启动本机连接。{}", crate::ensure::INSTALL_HINT)
    })?;
    let input = Arc::new(AsyncMutex::new(child.stdin.take().ok_or("无法打开输入。")?));
    let queue = Arc::new(Mutex::new(Queue { frames: vec![], bytes: 0, error: None, touched: Instant::now() }));
    let reader_queue = Arc::clone(&queue);
    let reader = tokio::spawn(async move {
        let result = read_frames(&mut child, &reader_queue).await;
        if let Ok(mut q) = reader_queue.lock() { q.error = Some(result); }
        let _ = child.kill().await;
        let _ = child.wait().await;
    });
    Ok(Session { input, queue, reader })
}

async fn read_frames(child: &mut Child, queue: &Arc<Mutex<Queue>>) -> String {
    let Some(output) = child.stdout.take() else { return "无法打开输出。".into() };
    let mut reader = BufReader::new(output);
    let mut tick = tokio::time::interval(Duration::from_secs(15));
    let mut raw = Vec::new();
    loop {
        let left = (MAX_FRAME + 1).saturating_sub(raw.len()) as u64;
        let mut limited = (&mut reader).take(left);
        tokio::select! {
            value = limited.read_until(b'\n', &mut raw) => {
                match value {
                    Ok(0) | Err(_) => return "连接已断开；任务结果待确认。".into(),
                    _ => {}
                }
                if raw.len() > MAX_FRAME { return "服务返回的数据过大。".into(); }
                let frame: Value = match serde_json::from_slice(&raw) {
                    Ok(f) => f, Err(_) => return "服务返回了无效数据。".into()
                };
                let Ok(mut q) = queue.lock() else { return "连接状态不可用。".into() };
                q.bytes += raw.len();
                if q.bytes > 4 * MAX_FRAME || q.frames.len() >= 4096 { return "输出积压，连接已关闭；任务结果待确认。".into(); }
                q.frames.push(frame); raw.clear();
            },
            _ = tick.tick() => {
                let Ok(q) = queue.lock() else { return "连接状态不可用。".into() };
                if q.touched.elapsed() > Duration::from_secs(90) { return "连接已过期。".into(); }
            }
        }
    }
}

#[tauri::command]
pub async fn session_send(state: tauri::State<'_, Sessions>, session: String, frame: Value) -> Result<(), String> {
    let input = {
        let map = state.0.lock().map_err(|_| "连接状态不可用。")?;
        let s = map.get(&session).ok_or("连接已关闭。")?;
        Arc::clone(&s.input)
    };
    let mut raw = serde_json::to_vec(&frame).map_err(|_| "无效请求。")?;
    raw.push(b'\n');
    if raw.len() > MAX_FRAME { return Err("请求过大。".into()); }
    let mut writer = input.lock().await;
    tokio::time::timeout(Duration::from_secs(5), writer.write_all(&raw)).await
        .map_err(|_| "发送超时；结果待确认。".to_string())?
        .map_err(|_| "连接已断开；结果待确认。".to_string())
}

#[tauri::command]
pub fn session_poll(state: tauri::State<'_, Sessions>, session: String) -> Result<Batch, String> {
    let mut map = state.0.lock().map_err(|_| "连接状态不可用。")?;
    let result = {
        let s = map.get(&session).ok_or("连接已关闭。")?;
        let mut q = s.queue.lock().map_err(|_| "连接状态不可用。")?;
        q.touched = Instant::now(); q.bytes = 0;
        Batch { frames: std::mem::take(&mut q.frames), error: q.error.clone() }
    };
    if result.error.is_some() { map.remove(&session); }
    Ok(result)
}

#[tauri::command]
pub fn session_close(state: tauri::State<'_, Sessions>, session: String) -> Result<(), String> {
    state.0.lock().map_err(|_| "连接状态不可用。")?.remove(&session);
    Ok(())
}

#[cfg(test)]
#[path = "session_tests.rs"]
mod tests;
