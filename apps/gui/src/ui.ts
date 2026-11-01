export function esc(s: string): string {
  return s.replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]!));
}
export function feedback(root: HTMLElement, error: unknown, bad = true) {
  const node = root.querySelector<HTMLElement>("[role=status]");
  if (node) { node.textContent = error instanceof Error ? error.message : String(error); node.classList.toggle("err", bad); }
}
export function action(root: HTMLElement, selector: string, fn: () => Promise<unknown>) {
  root.querySelector<HTMLButtonElement>(selector)?.addEventListener("click", async e => {
    const button = e.currentTarget as HTMLButtonElement;
    button.disabled = true;
    try { await fn(); } catch (e) { feedback(root, e); } finally { button.disabled = false; }
  });
}
export function form(root: HTMLElement, selector: string, fn: (data: FormData) => Promise<unknown>) {
  root.querySelector<HTMLFormElement>(selector)!.onsubmit = async e => {
    e.preventDefault(); const target = e.currentTarget as HTMLFormElement;
    const button = target.querySelector<HTMLButtonElement>("button[type=submit]")!;
    button.disabled = true;
    try { await fn(new FormData(target)); } catch (e) { feedback(root, e); } finally { button.disabled = false; }
  };
}
export function pageError(root: HTMLElement, e: unknown) {
  root.innerHTML = `<p role="alert" class="err">${esc(e instanceof Error ? e.message : "加载失败。")}</p>`;
}
