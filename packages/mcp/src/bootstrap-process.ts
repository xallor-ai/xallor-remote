import { spawn, type ChildProcess, type SpawnOptions } from "node:child_process";
import { BootstrapCleanupError, type BootstrapSnapshot } from "./bootstrap-snapshot.js";
import { prepareBootstrapImage, type BootstrapImage } from "./bootstrap-image.js";

export type BootstrapProcessOptions = { env?: NodeJS.ProcessEnv; stdio: "inherit" | "pipe"; windowsHide?: boolean; timeoutMs?: number; stopTimeoutMs?: number };
export type BootstrapExit = { code: number | null; signal: NodeJS.Signals | null; stderr: string };
type SpawnBootstrap = (binary: string, args: readonly string[], options: SpawnOptions) => ChildProcess;

export function runSnapshot(snapshot: BootstrapSnapshot, args: readonly string[], options: BootstrapProcessOptions, start: SpawnBootstrap = spawn,
  prepare: typeof prepareBootstrapImage = prepareBootstrapImage): Promise<BootstrapExit> {
  return new Promise((resolve, reject) => {
    let child: ChildProcess;
    let image: BootstrapImage | undefined;
    try {
      snapshot.assertUnchanged();
      image = prepare(snapshot.binary, snapshot.sha256);
      child = start(image.executable, args, { env: options.env, argv0: snapshot.binary,
        stdio: options.stdio === "pipe" ? ["ignore", "pipe", "pipe", image.fd] : ["inherit", "inherit", "inherit", image.fd],
        windowsHide: options.windowsHide });
    } catch (error) {
      try { image?.close(); snapshot.cleanup(); reject(error); }
      catch (cleanupError) { reject(new BootstrapCleanupError(snapshot.directory, new AggregateError([error, cleanupError]))); }
      return;
    }
    let stderr = Buffer.alloc(0);
    let failure: Error | undefined;
    let exited = false;
    let closed = false;
    let settled = false;
    let stopRequested = false;
    let spawned = child.pid !== undefined;
    let deadline: NodeJS.Timeout | undefined;
    let closeDeadline: NodeJS.Timeout | undefined;
    child.stderr?.on("data", (chunk: Buffer | string) => {
      const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
      stderr = Buffer.concat([stderr, bytes.subarray(-65536)]).subarray(-65536);
    });
    child.stdout?.resume();
    const finish = (code: number | null, signal: NodeJS.Signals | null) => {
      if (closed) return;
      closed = true;
      clearTimeout(deadline); clearTimeout(closeDeadline);
      let cleanupFailure: Error | undefined;
      try { image?.close(); snapshot.cleanup(); }
      catch (error) { cleanupFailure = error instanceof BootstrapCleanupError ? error : new BootstrapCleanupError(snapshot.directory, error); }
      // A late close can safely release a previously retained snapshot. It
      // cannot change a timeout/uncertainty already reported to the caller.
      if (settled) return;
      settled = true;
      if (cleanupFailure) reject(cleanupFailure);
      else if (failure) reject(failure);
      else resolve({ code, signal, stderr: stderr.toString("utf8") });
    };
    const awaitClose = () => {
      if (closed || closeDeadline) return;
      closeDeadline = setTimeout(() => {
        if (closed || settled) return;
        settled = true;
        // Only detach our observation resources. No second kill or candidate
        // launch is allowed, and no image is deleted without close evidence.
        child.unref();
        child.stdin?.destroy(); child.stdout?.destroy(); child.stderr?.destroy();
        reject(new BootstrapCleanupError(snapshot.directory, failure ?? new Error("Bootstrap exit unconfirmed")));
      }, options.stopTimeoutMs ?? 5000);
    };
    const stopOnce = () => {
      if (!stopRequested && !exited && child.pid !== undefined) {
        stopRequested = true;
        try { child.kill(); }
        catch (error) { failure ??= error instanceof Error ? error : new Error(String(error)); }
      }
      awaitClose();
    };
    child.once("spawn", () => { spawned = true; });
    child.once("exit", () => { exited = true; });
    child.once("close", finish);
    child.on("error", error => {
      failure ??= error;
      clearTimeout(deadline);
      if (!spawned && child.pid === undefined) finish(null, null);
      else stopOnce();
    });
    if (options.timeoutMs) {
      deadline = setTimeout(() => {
        failure ??= new Error("Runtime 启动结果尚未确认，请检查 xallor-remote runtime status。");
        stopOnce();
      }, options.timeoutMs);
    }
  });
}
