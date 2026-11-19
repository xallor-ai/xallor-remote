type Connection = { readonly isConnected: boolean; close(): void };

// Owns a head's admitted connection, not Runtime processes or installation state.
export function runtimeSession<T extends Connection>(
  initialize: (bootstrap: () => Promise<void>) => Promise<T>,
  bootstrap: () => Promise<void>,
): () => Promise<T> {
  let session: T | undefined;
  let pending: Promise<T> | undefined;
  let bootstrapAttempt: Promise<void> | undefined;
  // Retain both success and failure of the one permitted bootstrap attempt.
  // New tool calls can observe IPC again, but never replay process startup.
  const bootstrapOnce = () => bootstrapAttempt ??= Promise.resolve().then(bootstrap);
  return () => {
    if (session?.isConnected) return Promise.resolve(session);
    if (pending) return pending;
    session?.close();
    session = undefined;
    const attempt = Promise.resolve().then(() => initialize(bootstrapOnce)).then(connection => {
      if (!connection.isConnected) {
        connection.close();
        throw new Error("Runtime 就绪连接已断开，请检查本机运行状态。");
      }
      session = connection;
      return connection;
    });
    pending = attempt;
    // No detached rejecting finally promise; both settlement handlers succeed.
    void attempt.then(() => { pending = undefined; }, () => { pending = undefined; });
    return attempt;
  };
}
