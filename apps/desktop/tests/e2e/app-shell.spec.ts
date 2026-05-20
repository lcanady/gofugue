/**
 * App shell structure tests.
 *
 * Verifies that the four-pane layout (WorldTabs → TerminalPane → InputBar →
 * StatusBar) renders correctly and that global keyboard shortcuts work.
 * Most of these tests run in the "IPC disconnected" state — no mock needed.
 */
import { test, expect } from './fixtures/mock-ipc.js';

test.describe('App shell — structure', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
  });

  test('world tabs bar is visible', async ({ page }) => {
    await expect(page.getByRole('tablist', { name: 'Open worlds' })).toBeVisible();
  });

  test('new connection (+) button is visible in the tab bar', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'Open new connection' })).toBeVisible();
  });

  test('command input is visible', async ({ page }) => {
    await expect(page.getByRole('textbox', { name: 'Command input' })).toBeVisible();
  });

  test('command input receives autofocus on load', async ({ page }) => {
    await expect(page.getByRole('textbox', { name: 'Command input' })).toBeFocused();
  });

  test('send button is visible', async ({ page }) => {
    await expect(page.getByRole('button', { name: 'Send command' })).toBeVisible();
  });

  test('status bar is visible', async ({ page }) => {
    await expect(page.getByRole('status')).toBeVisible();
  });

  test('status bar always shows the GoFugue wordmark', async ({ page }) => {
    await expect(page.getByRole('status')).toContainText('GoFugue');
  });

  test('status bar shows "no world" when no world is active', async ({ page }) => {
    await expect(page.getByRole('status')).toContainText('no world');
  });
});

test.describe('App shell — empty state', () => {
  test('shows "Connecting to gofugue" when IPC is not yet connected', async ({ page }) => {
    // No mock → the WS connect will fail → the app shows the connecting state
    await page.goto('/');
    await expect(page.getByText(/connecting to gofugue/i)).toBeVisible();
    await expect(page.getByText(/gf --headless/i)).toBeVisible();
  });

  test('shows "No world connected" + connect link when IPC is up but no worlds', async ({
    page,
    mockIPC,
  }) => {
    await page.goto('/');
    await mockIPC.waitConnected();
    // IPC is connected but no hook events → no world tab → empty state
    await expect(page.getByText(/no world connected/i)).toBeVisible();
    await expect(page.getByRole('button', { name: /open a connection/i })).toBeVisible();
  });
});

test.describe('App shell — keyboard shortcuts', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
  });

  test('Ctrl+N opens the world manager dialog', async ({ page }) => {
    await page.keyboard.press('Control+n');
    await expect(page.getByRole('dialog', { name: 'Worlds' })).toBeVisible({
      timeout: 2_000,
    });
  });

  test('Escape closes the world manager dialog', async ({ page }) => {
    await page.keyboard.press('Control+n');
    const dialog = page.getByRole('dialog', { name: 'Worlds' });
    await expect(dialog).toBeVisible({ timeout: 2_000 });
    await page.keyboard.press('Escape');
    await expect(dialog).not.toBeVisible();
  });

  test('world manager shows New World button', async ({ page }) => {
    await page.keyboard.press('Control+n');
    await page.getByRole('dialog', { name: 'Worlds' }).waitFor({ timeout: 2_000 });
    await expect(page.getByRole('button', { name: /new world/i })).toBeVisible();
  });
});
