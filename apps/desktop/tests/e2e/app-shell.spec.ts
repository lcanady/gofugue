import { test, expect } from './fixtures/mock-ipc.js';
import { hookConnect } from './helpers/ipc-events.js';

test.describe('App shell — structure', () => {
  test('world tabs bar is visible when a world is connected', async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();
    mockIPC.pushEvent('hook', hookConnect('avalon'));
    await expect(page.locator('.lm_header')).toBeVisible();
  });

  test('command input is visible', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('textbox', { name: 'Command input' })).toBeVisible();
  });

  test('command input receives autofocus on load', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('textbox', { name: 'Command input' })).toBeFocused();
  });

  test('send button is visible', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('button', { name: 'Send command' })).toBeVisible();
  });

  test('status bar is visible', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('status')).toBeVisible();
  });

  test('status bar always shows the GoFugue wordmark', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('status')).toContainText('GoFugue');
  });

  test('status bar shows "no world" when no world is active', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('status')).toContainText('no world');
  });
});

test.describe('App shell — empty state', () => {
  test('shows connection status when IPC is not yet connected', async ({ page }) => {
    // Route WS connection to fail so it remains disconnected/connecting
    await page.routeWebSocket('ws://127.0.0.1:7879/', (ws) => {
      ws.close();
    });
    await page.goto('/');
    await expect(page.getByRole('status')).toContainText(/(connecting|disconnected)/i);
  });

  test('shows Connection service is not running when dialog is open and IPC is disconnected', async ({ page }) => {
    await page.routeWebSocket('ws://127.0.0.1:7879/', (ws) => {
      ws.close();
    });
    await page.goto('/');
    await page.keyboard.press('Control+Shift+n');
    await expect(page.getByRole('dialog', { name: 'Worlds' })).toBeVisible({ timeout: 2_000 });
    await expect(page.getByText(/Connection service is not running/i)).toBeVisible();
  });

  test('shows "No worlds connected" + connect link when IPC is up but no worlds', async ({
    page,
    mockIPC,
  }) => {
    await page.goto('/');
    await mockIPC.waitConnected();
    // IPC is connected but no hook events → no world tab → empty state
    await expect(page.getByText(/no worlds connected/i)).toBeVisible();
    await expect(page.getByRole('button', { name: /connect world/i })).toBeVisible();
  });
});

test.describe('App shell — keyboard shortcuts', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
  });

  test('Ctrl+Shift+N opens the world manager dialog', async ({ page }) => {
    await page.keyboard.press('Control+Shift+n');
    await expect(page.getByRole('dialog', { name: 'Worlds' })).toBeVisible({
      timeout: 2_000,
    });
  });

  test('Escape closes the world manager dialog', async ({ page }) => {
    await page.keyboard.press('Control+Shift+n');
    const dialog = page.getByRole('dialog', { name: 'Worlds' });
    await expect(dialog).toBeVisible({ timeout: 2_000 });
    await page.keyboard.press('Escape');
    await expect(dialog).not.toBeVisible();
  });

  test('world manager shows New World button', async ({ page }) => {
    await page.keyboard.press('Control+Shift+n');
    await page.getByRole('dialog', { name: 'Worlds' }).waitFor({ timeout: 2_000 });
    await expect(page.getByRole('button', { name: /new world/i })).toBeVisible();
  });
});
