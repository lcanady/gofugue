/**
 * Content-Security-Policy strings for the Electron renderer.
 *
 * These are exported as named constants so they can be unit-tested without
 * importing the full main-process entry point (which depends on Electron APIs).
 */

/**
 * Production CSP — strict; no eval, no inline scripts.
 */
export const CSP_PROD = [
  "default-src 'self'",
  "script-src 'self'",
  "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
  "font-src 'self' https://fonts.gstatic.com",
  "connect-src ws://127.0.0.1:7879 wss://127.0.0.1:7879",
  "img-src 'self' data:",
].join('; ');

/**
 * Development CSP — allows Vite's HMR WebSocket on localhost and the
 * `'unsafe-eval'` / `'unsafe-inline'` that React Fast Refresh requires.
 * More permissive than prod but still enforces a policy (no CSP at all in dev
 * would silently accept any injected script).
 */
export const CSP_DEV = [
  "default-src 'self'",
  "script-src 'self' 'unsafe-eval' 'unsafe-inline'",
  "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
  "font-src 'self' https://fonts.gstatic.com",
  "connect-src 'self' ws://localhost:* ws://127.0.0.1:* wss://127.0.0.1:7879",
  "img-src 'self' data:",
].join('; ');
