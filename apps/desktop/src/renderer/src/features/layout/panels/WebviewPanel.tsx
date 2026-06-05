/**
 * WebviewPanel — Chromium webview for external sites / code tools.
 * Uses Electron's <webview> tag (enabled via webviewTag: true in BrowserWindow config).
 *
 * Security: only https:// and file:// URLs are allowed. The main process
 * enforces the same allowlist via the will-attach-webview handler, so this
 * check is an additional defence-in-depth layer in the renderer.
 */

/** Returns true only for URL schemes safe to load in a <webview>. */
export function isAllowedWebviewUrl(url: string): boolean {
  if (!url) return false;
  return url.startsWith('https://') || url.startsWith('file://');
}

interface Props {
  url: string;
}

export function WebviewPanel({ url }: Props) {
  if (!url) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground text-xs font-mono">
        No URL set
      </div>
    );
  }

  if (!isAllowedWebviewUrl(url)) {
    return (
      <div className="flex items-center justify-center h-full text-destructive text-xs font-mono">
        Blocked: only https:// and file:// URLs are allowed in the webview.
      </div>
    );
  }

  return (
    <webview
      src={url}
      className="w-full h-full"
      style={{ display: 'flex' }}
    />
  );
}
