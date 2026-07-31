// 极简 toast（无第三方依赖）。使用 textContent 赋值，天然防 XSS。
let container: HTMLDivElement | null = null;

function ensureContainer(): HTMLDivElement {
  if (!container) {
    container = document.createElement('div');
    container.style.cssText =
      'position:fixed;top:16px;left:50%;transform:translateX(-50%);z-index:9999;display:flex;flex-direction:column;gap:8px;align-items:center;';
    document.body.appendChild(container);
  }
  return container;
}

export function toast(message: string, type: 'error' | 'success' = 'error'): void {
  const el = document.createElement('div');
  el.textContent = message; // 只用 textContent，不用 innerHTML
  el.style.cssText =
    'padding:10px 16px;border-radius:8px;color:#fff;font-size:14px;max-width:80vw;box-shadow:0 4px 12px rgba(0,0,0,.15);' +
    (type === 'error' ? 'background:#e5484d;' : 'background:#30a46c;');
  ensureContainer().appendChild(el);
  setTimeout(() => el.remove(), 3000);
}
