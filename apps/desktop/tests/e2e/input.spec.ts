/**
 * Input bar behaviour tests.
 *
 * Verifies typing, submission, history navigation (↑/↓), and correct routing
 * of /commands vs plain text through the gofugue IPC.
 */
import { test, expect } from './fixtures/mock-ipc.js';
import { hookConnect } from './helpers/ipc-events.js';

test.describe('Input bar — basic interaction', () => {
  test.beforeEach(async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();
    mockIPC.pushEvent('hook', hookConnect('testworld'));
    await page.getByRole('tab', { name: /testworld/ }).waitFor({ timeout: 2_000 });
  });

  test('input field is autofocused', async ({ page }) => {
    await expect(page.getByRole('textbox', { name: 'Command input' })).toBeFocused();
  });

  test('typing updates the input value', async ({ page }) => {
    const input = page.getByRole('textbox', { name: 'Command input' });
    await input.type('look north');
    await expect(input).toHaveValue('look north');
  });

  test('send button is disabled when input is empty', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'Send command' })).toBeDisabled();
  });

  test('send button is enabled when input has non-whitespace text', async ({ page }) => {
    await page.getByRole('textbox', { name: 'Command input' }).type('look');
    await expect(page.getByRole('button', { name: 'Send command' })).toBeEnabled();
  });

  test('Enter clears the input field after submit', async ({ page }) => {
    const input = page.getByRole('textbox', { name: 'Command input' });
    await input.type('look');
    await input.press('Enter');
    await expect(input).toHaveValue('');
  });

  test('clicking Send clears the input field', async ({ page }) => {
    const input = page.getByRole('textbox', { name: 'Command input' });
    await input.type('look');
    await page.getByRole('button', { name: 'Send command' }).click();
    await expect(input).toHaveValue('');
  });
});

test.describe('Input bar — IPC routing', () => {
  test.beforeEach(async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();
    mockIPC.pushEvent('hook', hookConnect('testworld'));
    await page.getByRole('tab', { name: /testworld/ }).waitFor({ timeout: 2_000 });
  });

  test('plain text dispatches an "input" JSON-RPC call', async ({ page, mockIPC }) => {
    const input = page.getByRole('textbox', { name: 'Command input' });
    await input.type('look north');
    await input.press('Enter');

    await expect
      .poll(() => mockIPC.messages, { timeout: 2_000 })
      .toEqual(
        expect.arrayContaining([
          expect.objectContaining({
            method: 'input',
            params: expect.objectContaining({ text: 'look north' }),
          }),
        ]),
      );
  });

  test('/command dispatches a "cmd" JSON-RPC call', async ({ page, mockIPC }) => {
    const input = page.getByRole('textbox', { name: 'Command input' });
    await input.type('/connect mud://example.com:4000');
    await input.press('Enter');

    await expect
      .poll(() => mockIPC.messages, { timeout: 2_000 })
      .toEqual(
        expect.arrayContaining([
          expect.objectContaining({
            method: 'cmd',
            params: expect.objectContaining({ line: '/connect mud://example.com:4000' }),
          }),
        ]),
      );
  });

  test('whitespace-only input is not dispatched', async ({ page, mockIPC }) => {
    const initialCount = mockIPC.messages.length;
    const input = page.getByRole('textbox', { name: 'Command input' });
    await input.type('   ');
    await input.press('Enter');
    // Give it a moment to dispatch (it shouldn't)
    await page.waitForTimeout(200);
    expect(mockIPC.messages.length).toBe(initialCount);
  });
});

test.describe('Input bar — command history', () => {
  test.beforeEach(async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();
    mockIPC.pushEvent('hook', hookConnect('testworld'));
    await page.getByRole('tab', { name: /testworld/ }).waitFor({ timeout: 2_000 });
  });

  test('ArrowUp recalls the last submitted command', async ({ page }) => {
    const input = page.getByRole('textbox', { name: 'Command input' });
    await input.type('look');
    await input.press('Enter');
    await input.press('ArrowUp');
    await expect(input).toHaveValue('look');
  });

  test('ArrowUp through multiple entries restores each in order', async ({ page }) => {
    const input = page.getByRole('textbox', { name: 'Command input' });
    for (const cmd of ['first', 'second', 'third']) {
      await input.type(cmd);
      await input.press('Enter');
    }
    await input.press('ArrowUp'); // third
    await expect(input).toHaveValue('third');
    await input.press('ArrowUp'); // second
    await expect(input).toHaveValue('second');
    await input.press('ArrowUp'); // first
    await expect(input).toHaveValue('first');
  });

  test('ArrowDown after ArrowUp restores draft text', async ({ page }) => {
    const input = page.getByRole('textbox', { name: 'Command input' });
    await input.type('oldcmd');
    await input.press('Enter');
    await input.type('draft');
    await input.press('ArrowUp'); // recall oldcmd
    await expect(input).toHaveValue('oldcmd');
    await input.press('ArrowDown'); // restore draft
    await expect(input).toHaveValue('draft');
  });

  test('ArrowUp at top of history does not go past last entry', async ({ page }) => {
    const input = page.getByRole('textbox', { name: 'Command input' });
    await input.type('only');
    await input.press('Enter');
    await input.press('ArrowUp');
    await input.press('ArrowUp'); // no more — should stay at "only"
    await expect(input).toHaveValue('only');
  });
});
