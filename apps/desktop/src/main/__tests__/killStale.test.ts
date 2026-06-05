import { describe, expect, it } from 'vitest';
import { parsePidsUnix, parsePidsWindows, MAX_PID } from '../killStale.js';

// ---------------------------------------------------------------------------
// L-3: PID parsing for killStaleGofugue()
//
// parsePidsUnix / parsePidsWindows must only return strictly-positive
// integers less than MAX_PID (2^22 = 4 194 304).  Any other token must be
// silently dropped so that process.kill() is never called with NaN, 0, a
// negative number, or an out-of-range value — regardless of what lsof /
// netstat prints.
// ---------------------------------------------------------------------------

describe('parsePidsUnix', () => {
  it('returns valid PIDs from normal lsof -ti output', () => {
    const output = '1234\n5678\n9000\n';
    expect(parsePidsUnix(output)).toEqual([1234, 5678, 9000]);
  });

  it('ignores empty lines', () => {
    expect(parsePidsUnix('\n\n1234\n\n')).toEqual([1234]);
  });

  it('ignores non-numeric tokens', () => {
    expect(parsePidsUnix('abc\n1234\nfoo bar\n')).toEqual([1234]);
  });

  it('ignores PID 0', () => {
    expect(parsePidsUnix('0\n1234\n')).toEqual([1234]);
  });

  it('ignores negative numbers (leading minus is non-digit)', () => {
    expect(parsePidsUnix('-1\n1234\n')).toEqual([1234]);
  });

  it('ignores PIDs at or above MAX_PID', () => {
    const aboveMax = String(MAX_PID);
    const belowMax = String(MAX_PID - 1);
    expect(parsePidsUnix(`${aboveMax}\n${belowMax}\n`)).toEqual([MAX_PID - 1]);
  });

  it('ignores floats / decimal points', () => {
    expect(parsePidsUnix('12.34\n1234\n')).toEqual([1234]);
  });

  it('returns empty array for entirely empty output', () => {
    expect(parsePidsUnix('')).toEqual([]);
  });

  it('returns empty array when no valid PIDs are present', () => {
    expect(parsePidsUnix('no pids here\nheader line\n')).toEqual([]);
  });

  it('strips leading/trailing whitespace from each token', () => {
    expect(parsePidsUnix('  1234  \n  5678  \n')).toEqual([1234, 5678]);
  });
});

describe('parsePidsWindows', () => {
  const netstatOutput = [
    '',
    'Active Connections',
    '',
    '  Proto  Local Address          Foreign Address        State           PID',
    '  TCP    0.0.0.0:7878           0.0.0.0:0              LISTENING       1234',
    '  TCP    0.0.0.0:7879           0.0.0.0:0              LISTENING       5678',
    '  TCP    0.0.0.0:80             0.0.0.0:0              LISTENING       9000',
    '  TCP    0.0.0.0:443            0.0.0.0:0              LISTENING       9001',
  ].join('\n');

  it('returns PIDs only for lines with port 7878 or 7879', () => {
    expect(parsePidsWindows(netstatOutput)).toEqual([1234, 5678]);
  });

  it('deduplicates PIDs that appear on multiple matching lines', () => {
    const dup = [
      '  TCP    0.0.0.0:7878    0.0.0.0:0    LISTENING    1234',
      '  TCP    0.0.0.0:7879    0.0.0.0:0    LISTENING    1234',
    ].join('\n');
    expect(parsePidsWindows(dup)).toEqual([1234]);
  });

  it('ignores header lines (non-numeric last field)', () => {
    const header = '  Proto  Local Address  Foreign Address  State  PID\n  TCP    0.0.0.0:7878  0.0.0.0:0  LISTENING  1234\n';
    expect(parsePidsWindows(header)).toEqual([1234]);
  });

  it('ignores PID 0', () => {
    const line = '  TCP    0.0.0.0:7878    0.0.0.0:0    LISTENING    0\n';
    expect(parsePidsWindows(line)).toEqual([]);
  });

  it('ignores negative PID in last field', () => {
    const line = '  TCP    0.0.0.0:7878    0.0.0.0:0    LISTENING    -5\n';
    expect(parsePidsWindows(line)).toEqual([]);
  });

  it('ignores PIDs at or above MAX_PID', () => {
    const line = `  TCP    0.0.0.0:7878    0.0.0.0:0    LISTENING    ${MAX_PID}\n`;
    expect(parsePidsWindows(line)).toEqual([]);
  });

  it('returns empty array when output is empty', () => {
    expect(parsePidsWindows('')).toEqual([]);
  });

  it('returns empty array when no port 7878/7879 lines exist', () => {
    const line = '  TCP    0.0.0.0:8080    0.0.0.0:0    LISTENING    9999\n';
    expect(parsePidsWindows(line)).toEqual([]);
  });
});
