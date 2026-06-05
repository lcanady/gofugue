/**
 * Returns true only for URLs safe to open in the user's default browser.
 *
 * shell.openExternal forwards the URL to the OS, which hands it to whatever
 * application is registered for that protocol — including potentially harmful
 * handlers for javascript:, file://, app://, and data: URIs. Restricting to
 * http:// and https:// eliminates that surface.
 */
export function isAllowedExternalUrl(url: string): boolean {
  return url.startsWith('https://') || url.startsWith('http://');
}

/**
 * Returns true only for URLs safe to load inside an Electron <webview> tag.
 *
 * The <webview> tag runs a full renderer process. Allowing arbitrary schemes
 * lets an attacker load javascript: URIs (XSS), file:// paths (local file
 * read), or plaintext http:// (MITM). Only https:// and file:// are
 * permitted; http:// is intentionally excluded because non-TLS content can
 * be intercepted on the wire.
 */
export function isAllowedWebviewUrl(url: string): boolean {
  if (!url) return false;
  return url.startsWith('https://') || url.startsWith('file://');
}
