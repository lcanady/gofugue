import { describe, expect, it } from 'vitest';
import { isAllowedExternalUrl } from '../security.js';

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
