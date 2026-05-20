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
