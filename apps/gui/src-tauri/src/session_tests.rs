use super::*;
use serde_json::json;

async fn send(s: &Session, id: &str, method: &str, params: Value) {
    let raw = format!("{}\n", json!({"id":id,"method":method,"params":params}));
    s.input.lock().await.write_all(raw.as_bytes()).await.unwrap();
}
async fn next(s: &Session) -> Value {
    tokio::time::timeout(Duration::from_secs(30), async {
        loop {
            {
                let mut q = s.queue.lock().unwrap();
                assert!(q.error.is_none(), "{:?}", q.error);
                if !q.frames.is_empty() { return q.frames.remove(0); }
            }
            tokio::time::sleep(Duration::from_millis(20)).await;
        }
    }).await.expect("native bridge response timeout")
}

// Exercise the actual native child bridge against the same isolated dual Runtime fixture.
#[tokio::test]
#[ignore = "requires tests/gui_e2e.py --native fixture"]
async fn native_bridge_should_multiplex_and_cancel_on_its_connection() {
    let s = launch().unwrap();
    send(&s,"status","status",json!({})).await;
    let frame = next(&s).await;
    assert_eq!(frame["result"]["device_id"], std::env::var("XR_GUI_CONTROLLER").unwrap());
    send(&s,"subscribe","approval.subscribe",json!({})).await;
    assert_eq!(next(&s).await["ok"], true);
    let command = if cfg!(windows) {"Write-Output native-stream; Start-Sleep -Seconds 30"} else {"echo native-stream; sleep 30"};
    send(&s,"exec","exec",json!({"device_id":std::env::var("XR_GUI_TARGET").unwrap(),"command":command,"timeout_ms":45000})).await;
    let mut exec_id = String::new();
    loop {
        let frame = next(&s).await;
        if let Some(id)=frame["exec_id"].as_str() {exec_id=id.into();}
        if frame["data"].as_str().unwrap_or("").contains("native-stream") {break;}
        assert!(frame["ok"].is_null(), "terminal before output: {frame}");
    }
    assert!(!exec_id.is_empty());
    send(&s,"cancel","exec.cancel",json!({"exec_id":exec_id})).await;
    let mut ack=false; let mut exit=false;
    while !ack || !exit {
        let frame = next(&s).await;
        if frame["id"] == "cancel" {assert_eq!(frame["ok"],true);ack=true;}
        if frame["id"] == "exec" && !frame["ok"].is_null() {assert_eq!(frame["result"]["status"],"cancelled");exit=true;}
    }
    drop(s);
}
