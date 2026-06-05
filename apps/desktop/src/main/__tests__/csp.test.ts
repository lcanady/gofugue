import { describe, expect, it } from 'vitest';
import { CSP_PROD, CSP_DEV } from '../csp.js';

// ---------------------------------------------------------------------------
// Security: Content-Security-Policy strings (M-2)
//
// Verifies that:
//  - A CSP is defined for BOTH production and development (no env skips).
//  - The production CSP does NOT contain 'unsafe-eval' or 'unsafe-inline'
//    in the script-src directive.
//  - Both policies include a connect-src that allows the gofugue WebSocket.
//  - The development CSP, while more permissive for HMR, still has a
//    defined script-src (it does NOT omit the directive entirely).
// ---------------------------------------------------------------------------

describe('CSP_PROD', () => {
  it('is a non-empty string', () => {
    expect(typeof CSP_PROD).toBe('string');
    expect(CSP_PROD.length).toBeGreaterThan(0);
  });

  it('script-src does NOT contain unsafe-eval', () => {
    // Extract the script-src directive only so we don't accidentally match
    // another directive that might legitimately contain the word "eval".
    const scriptSrc = CSP_PROD.split(';')
      .map((d) => d.trim())
      .find((d) => d.startsWith('script-src'));
    expect(scriptSrc).toBeDefined();
    expect(scriptSrc).not.toContain("'unsafe-eval'");
  });

  it('script-src does NOT contain unsafe-inline', () => {
    const scriptSrc = CSP_PROD.split(';')
      .map((d) => d.trim())
      .find((d) => d.startsWith('script-src'));
    expect(scriptSrc).not.toContain("'unsafe-inline'");
  });

  it('allows the gofugue WebSocket endpoint in connect-src', () => {
    expect(CSP_PROD).toContain('ws://127.0.0.1:7879');
  });

  it('contains a default-src directive', () => {
    expect(CSP_PROD).toContain("default-src 'self'");
  });
});

describe('CSP_DEV', () => {
  it('is a non-empty string', () => {
    expect(typeof CSP_DEV).toBe('string');
    expect(CSP_DEV.length).toBeGreaterThan(0);
  });

  it('has an explicit script-src directive (CSP is not omitted in dev)', () => {
    const scriptSrc = CSP_DEV.split(';')
      .map((d) => d.trim())
      .find((d) => d.startsWith('script-src'));
    expect(scriptSrc).toBeDefined();
  });

  it('allows localhost WebSockets in connect-src for HMR', () => {
    expect(CSP_DEV).toContain('ws://localhost:*');
  });

  it('contains a default-src directive', () => {
    expect(CSP_DEV).toContain("default-src 'self'");
  });
});
