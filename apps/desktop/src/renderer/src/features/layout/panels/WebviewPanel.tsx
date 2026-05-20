/**
 * WebviewPanel — Chromium webview for external sites / code tools.
 * Uses Electron's <webview> tag (enabled via webviewTag: true in BrowserWindow config).
 */

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

  return (
    <webview
      src={url}
      className="w-full h-full"
      style={{ display: 'flex' }}
    />
  );
}
