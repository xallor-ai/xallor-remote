export function readToolText(content: unknown, truncated: unknown): string {
  const body = typeof content === "string" ? content : "";
  if (truncated !== true) {
    return body;
  }
  const hint = "内容已截断，不是全文。用 offset、limit 或 head/tail 接着读。";
  return body ? `${body}\n\n${hint}` : hint;
}
