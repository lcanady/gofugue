/**
 * PID parsing utilities for killStaleGofugue().
 *
 * Extracted as pure functions so they can be unit-tested without spawning
 * child processes or touching Electron APIs.
 */

/** Maximum valid PID. Linux allows up to 4 194 304 (2^22); we use that as a
 *  safe upper bound on all platforms. */
export const MAX_PID = 4_194_304;

/**
 * Parse PIDs from lsof -ti output (Unix).
 *
 * lsof -ti prints one PID per line.  We accept only tokens that:
 *   - consist entirely of ASCII digits (/^\d+$/)
 *   - convert to an integer > 0 and < MAX_PID
 *
 * Any other token (header line, empty string, negative number, etc.) is
 * silently discarded.
 */
export function parsePidsUnix(output: string): number[] {
  const result: number[] = [];
  for (const token of output.split('\n')) {
    const t = token.trim();
    if (!/^\d+$/.test(t)) continue;
    const n = Number(t);
    if (n > 0 && n < MAX_PID) {
      result.push(n);
    }
  }
  return result;
}

/**
 * Parse PIDs from `netstat -ano` output (Windows).
 *
 * netstat output looks like:
 *   TCP    0.0.0.0:7878    0.0.0.0:0    LISTENING    1234
 *
 * We only look at lines containing ":7878" or ":7879", then take the last
 * whitespace-separated field as the PID.  The same strict numeric validation
 * as parsePidsUnix applies.
 */
export function parsePidsWindows(output: string): number[] {
  const result: number[] = [];
  for (const line of output.split('\n')) {
    if (!line.includes(':7878') && !line.includes(':7879')) continue;
    const parts = line.trim().split(/\s+/);
    const t = parts[parts.length - 1] ?? '';
    if (!/^\d+$/.test(t)) continue;
    const n = Number(t);
    if (n > 0 && n < MAX_PID) {
      if (!result.includes(n)) result.push(n);
    }
  }
  return result;
}
