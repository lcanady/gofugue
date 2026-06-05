import { describe, expect, it } from 'vitest';
import { isAllowedExternalUrl, isAllowedWebviewUrl } from '../security.js';

// ---------------------------------------------------------------------------
// Security: shell.openExternal protocol allowlist (L1)
//
// Red test — proves the vulnerability: without a guard, setWindowOpenHandler
// forwards any URL (including file:// and javascript:) to shell.openExternal.
// After the fix, only http:// and https:// are allowed.
// ---------------------------------------------------------------------------

describe('isAllowedExternalUrl', () => {
  it('allows http URLs', () => {
    expect(isAllowedExternalUrl('http://example.com')).toBe(true);
  });

  it('allows https URLs', () => {
    expect(isAllowedExternalUrl('https://example.com/path?q=1')).toBe(true);
  });

  it('blocks javascript: URIs', () => {
    expect(isAllowedExternalUrl('javascript:alert(1)')).toBe(false);
  });

  it('blocks file:// URIs', () => {
    expect(isAllowedExternalUrl('file:///etc/passwd')).toBe(false);
  });

  it('blocks app:// deep links that could invoke native handlers', () => {
    expect(isAllowedExternalUrl('app://something')).toBe(false);
  });

  it('blocks data: URIs', () => {
    expect(isAllowedExternalUrl('data:text/html,<script>alert(1)</script>')).toBe(false);
  });

  it('blocks empty strings', () => {
    expect(isAllowedExternalUrl('')).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Security: webview src URL allowlist (H-2)
//
// isAllowedWebviewUrl guards the <webview> tag against loading dangerous
// schemes. Only https:// (TLS) and file:// (local) are permitted.
// ---------------------------------------------------------------------------

describe('isAllowedWebviewUrl', () => {
  it('allows https URLs', () => {
    expect(isAllowedWebviewUrl('https://example.com')).toBe(true);
  });

  it('allows https URLs with path and query', () => {
    expect(isAllowedWebviewUrl('https://example.com/path?q=1#frag')).toBe(true);
  });

  it('allows file:// URLs', () => {
    expect(isAllowedWebviewUrl('file:///path/to/file.html')).toBe(true);
  });

  it('rejects http:// (non-TLS)', () => {
    expect(isAllowedWebviewUrl('http://example.com')).toBe(false);
  });

  it('rejects javascript: URIs', () => {
    expect(isAllowedWebviewUrl('javascript:alert(1)')).toBe(false);
  });

  it('rejects data: URIs', () => {
    expect(isAllowedWebviewUrl('data:text/html,<script>alert(1)</script>')).toBe(false);
  });

  it('rejects empty string', () => {
    expect(isAllowedWebviewUrl('')).toBe(false);
  });
});
