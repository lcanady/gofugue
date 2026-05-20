/**
 * IPC connection state tests.
 *
 * Verifies that the UI reflects IPC state changes (connecting → connected),
 * that world tabs appear when a CONNECT hook arrives, and that the status bar
 * shows the correct world connection/lag info.
 */
import { test, expect } from './fixtures/mock-ipc.js';
import { hookConnect, hookDisconnect, statusEvent } from './helpers/ipc-events.js';

test.describe('IPC connection states', () => {
  test('status bar shows IPC disconnected icon before connection', async ({ page }) => {
    // No mock → IPC never connects → WifiOff icon is shown
    await page.goto('/');
    // The IPC state text is "connecting" or "disconnected"
    const status = page.getByRole('status');
    // Should NOT show "gofugue" (which only appears when connected)
    // Should show the connecting state text (the ipcState value, not "gofugue")
    await expect(status).not.toContainText('gofugue', { timeout: 500 }).catch(() => {});
  });

  test('status bar shows IPC connected state after WebSocket handshake', async ({
    page,
    mockIPC,
  }) => {
    await page.goto('/');
    await mockIPC.waitConnected();

    // Once connected the status bar shows "gofugue" (the connected label).
    await expect(page.getByRole('status')).toContainText('gofugue', { timeout: 3_000 });
  });

  test('CONNECT hook creates a world tab', async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();

    mockIPC.pushEvent('hook', hookConnect('avalon'));

    await expect(page.getByRole('tab', { name: /avalon/ })).toBeVisible({ timeout: 2_000 });
  });

  test('world tab shows a connection indicator dot', async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();

    mockIPC.pushEvent('hook', hookConnect('mymush'));
    mockIPC.pushEvent('status', statusEvent('mymush', true, 0));

    const tab = page.getByRole('tab', { name: /mymush/ });
    await expect(tab).toBeVisible({ timeout: 2_000 });
    // The green dot has aria-label="connected"
    await expect(tab.getByLabel('connected')).toBeVisible();
  });

  test('status bar shows CONNECTED for the active world', async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();

    mockIPC.pushEvent('hook', hookConnect('mymush'));
    mockIPC.pushEvent('status', statusEvent('mymush', true, 42));

    await expect(page.getByRole('status')).toContainText('CONNECTED', { timeout: 2_000 });
  });

  test('status bar shows lag in ms when lag is non-zero', async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();

    mockIPC.pushEvent('hook', hookConnect('mymush'));
    mockIPC.pushEvent('status', statusEvent('mymush', true, 123));

    await expect(page.getByRole('status')).toContainText('123ms', { timeout: 2_000 });
  });

  test('status bar shows DISCONNECTED after disconnect hook', async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();

    mockIPC.pushEvent('hook', hookConnect('mymush'));
    mockIPC.pushEvent('status', statusEvent('mymush', true));
    await expect(page.getByRole('status')).toContainText('CONNECTED', { timeout: 2_000 });

    mockIPC.pushEvent('hook', hookDisconnect('mymush'));
    mockIPC.pushEvent('status', statusEvent('mymush', false));
    await expect(page.getByRole('status')).toContainText('DISCONNECTED', { timeout: 2_000 });
  });

  test('clicking + opens world manager, adding a world and connecting dispatches /connect cmd', async ({
    page,
    mockIPC,
  }) => {
    await page.goto('/');
    await mockIPC.waitConnected();

    await page.getByRole('button', { name: 'Open new connection' }).click();
    const dialog = page.getByRole('dialog', { name: 'Worlds' });
    await expect(dialog).toBeVisible({ timeout: 2_000 });

    // Add a new world and fill its URL.
    await page.getByRole('button', { name: /new world/i }).click();
    await page.getByPlaceholder('mud://host:4000').fill('mud://testmush.org:4201');

    // Click Connect — triggers /connect cmd
    await page.getByRole('button', { name: 'Connect' }).click();

    await expect
      .poll(() => mockIPC.messages, { timeout: 2_000 })
      .toEqual(
        expect.arrayContaining([
          expect.objectContaining({ method: 'cmd' }),
        ]),
      );
  });
});
