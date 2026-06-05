/**
 * E2E tests for GoFugue Backend Feature Integrations:
 * - Global Variables (Variables tab, /set sync)
 * - Safety Restrictions (Safety tab, /restrict sync)
 * - TinyFugue Script Loader (Scripts tab, /load)
 * - Session Logging (StatusBar record button, /log cmd)
 * - Terminal Scrollback Search (Ctrl+F panel)
 */
import { test, expect } from './fixtures/mock-ipc.js';
import { hookConnect, statusEvent } from './helpers/ipc-events.js';

// Helper: toggle the search panel via the window handle exposed by App.tsx.
// This reliably triggers React's state update even in Playwright's Chromium,
// where native Ctrl+F is intercepted before reaching the page's JS event handlers.
async function pressCtrlF(page: import('@playwright/test').Page) {
  await page.evaluate(() => {
    (window as any).__gofugueToggleSearch?.();
  });
  // Small wait to let React flush the state update
  await page.waitForTimeout(50);
}

// ── World Manager — Variables Tab ─────────────────────────────────────────────

test.describe('World Manager — Variables Tab', () => {
  test.beforeEach(async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();
    await page.keyboard.press('Control+Shift+n');
    await page.getByRole('button', { name: /new world/i }).first().click();
  });

  test('Variables tab is visible in World Manager', async ({ page }) => {
    const varsTab = page.getByRole('button', { name: 'Variables' });
    await expect(varsTab).toBeVisible();
  });

  test('can navigate to the Variables tab', async ({ page }) => {
    await page.getByRole('button', { name: 'Variables' }).click();
    await expect(page.getByText(/Global Variables/i)).toBeVisible();
    await expect(page.getByText(/No variables defined yet/i)).toBeVisible();
  });

  test('can add a new variable', async ({ page }) => {
    await page.getByRole('button', { name: 'Variables' }).click();
    await page.getByRole('button', { name: 'Add Variable' }).click();

    const nameInput = page.getByPlaceholder('e.g. target');
    await expect(nameInput).toBeVisible();
    await nameInput.fill('target');

    const valueInput = page.getByPlaceholder('e.g. orc');
    await valueInput.fill('goblin');
  });

  test('can delete a variable', async ({ page }) => {
    await page.getByRole('button', { name: 'Variables' }).click();
    await page.getByRole('button', { name: 'Add Variable' }).click();
    await page.getByPlaceholder('e.g. target').fill('target');
    await page.getByPlaceholder('e.g. orc').fill('orc');

    // Delete the variable
    await page.locator('.text-muted-foreground.hover\\:text-destructive').click();
    await expect(page.getByText('No variables defined yet.')).toBeVisible();
  });

  test('multiple variables can be added', async ({ page }) => {
    await page.getByRole('button', { name: 'Variables' }).click();

    // Add first variable
    await page.getByRole('button', { name: 'Add Variable' }).click();
    const firstCard = page.locator('[data-testid="variable-card"]').first();
    await firstCard.getByPlaceholder('e.g. target').fill('target');
    await firstCard.getByPlaceholder('e.g. orc').fill('dragon');

    // Add second variable
    await page.getByRole('button', { name: 'Add Variable' }).click();
    const secondCard = page.locator('[data-testid="variable-card"]').nth(1);
    await secondCard.getByPlaceholder('e.g. target').fill('weapon');
    await secondCard.getByPlaceholder('e.g. orc').fill('sword');

    // Both should be visible
    await expect(page.locator('input[value="target"]')).toBeVisible();
    await expect(page.locator('input[value="weapon"]')).toBeVisible();
  });
});

// ── Variables Sync on Connect ──────────────────────────────────────────────────

test.describe('Variables — Backend Sync on Connect', () => {
  test('enabled variables dispatch /set commands on world connect', async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();

    // Connect to a world named 'realm'
    mockIPC.pushEvent('hook', hookConnect('realm'));
    mockIPC.pushEvent('status', statusEvent('realm', true));

    // Open dialog and configure a variable
    await page.keyboard.press('Control+Shift+n');
    await page.getByRole('button', { name: /new world/i }).first().click();
    await page.getByPlaceholder('My MUD').fill('realm');
    await page.getByPlaceholder('mud://host:4000').fill('mud://realm');

    // Add a variable
    await page.getByRole('button', { name: 'Variables' }).click();
    await page.getByRole('button', { name: 'Add Variable' }).click();
    await page.getByPlaceholder('e.g. target').fill('target');
    await page.getByPlaceholder('e.g. orc').fill('orc');

    // Close the dialog (triggers macro sync)
    await page.keyboard.press('Escape');

    // Wait for the /set command to be dispatched
    await expect
      .poll(() => mockIPC.messages, { timeout: 3000 })
      .toEqual(
        expect.arrayContaining([
          expect.objectContaining({
            method: 'cmd',
            params: expect.objectContaining({ line: '/set target=orc' }),
          }),
        ]),
      );
  });

  test('disabled variables do NOT dispatch /set commands', async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();

    mockIPC.pushEvent('hook', hookConnect('realm2'));
    mockIPC.pushEvent('status', statusEvent('realm2', true));

    await page.keyboard.press('Control+Shift+n');
    await page.getByRole('button', { name: /new world/i }).first().click();
    await page.getByPlaceholder('mud://host:4000').fill('mud://realm2');

    // Add a variable and disable it
    await page.getByRole('button', { name: 'Variables' }).click();
    await page.getByRole('button', { name: 'Add Variable' }).click();
    await page.getByPlaceholder('e.g. target').fill('notsent');
    await page.getByPlaceholder('e.g. orc').fill('value');

    // Uncheck the enabled checkbox in the first variable card
    const varCard = page.locator('[data-testid="variable-card"]').first();
    const enabledCheckbox = varCard.locator('input[type="checkbox"]');
    await enabledCheckbox.uncheck();

    await page.keyboard.press('Escape');

    // Allow time for any messages to arrive
    await page.waitForTimeout(500);

    const msgs = mockIPC.messages as Array<{ method?: string; params?: { line?: string } }>;
    const setMsgs = msgs.filter(
      (m) => m.method === 'cmd' && m.params?.line?.startsWith('/set notsent'),
    );
    expect(setMsgs).toHaveLength(0);
  });
});

// ── World Manager — Safety Tab ────────────────────────────────────────────────

test.describe('World Manager — Safety Tab', () => {
  test.beforeEach(async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();
    await page.keyboard.press('Control+Shift+n');
    await page.getByRole('button', { name: /new world/i }).first().click();
, async ({ page }) => {
    await expect(page.getByRole('button', { name: 'Safety' })).toBeVisible();
  });

  test('can navigate to the Safety tab', async ({ page }) => {
    await page.getByRole('button', { name: 'Safety' }).click();
    await expect(page.getByText(/Safety Restrictions/i)).toBeVisible();
  });

  test('shows all three restriction checkboxes', async ({ page }) => {
    await page.getByRole('button', { name: 'Safety' }).click();
    await expect(page.getByText(/Disallow Shell Execution/i)).toBeVisible();
    await expect(page.getByText(/Disallow File Access/i)).toBeVisible();
    await expect(page.getByText(/Disallow External Connections/i)).toBeVisible();
  });

  test('can toggle the SHELL restriction', async ({ page }) => {
    await page.getByRole('button', { name: 'Safety' }).click();
    // Find the SHELL restriction card and its checkbox
    const safetyCards = page.locator('.border.border-border.rounded-lg');
    const shellCard = safetyCards.filter({ hasText: /Disallow Shell Execution/i }).first();
    const shellCheckbox = shellCard.locator('input[type="checkbox"]');
    await shellCheckbox.check();
    await expect(shellCheckbox).toBeChecked();
  });

  test('safety restrictions dispatch /restrict commands on connect', async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();

    mockIPC.pushEvent('hook', hookConnect('safe-realm'));
    mockIPC.pushEvent('status', statusEvent('safe-realm', true));

    await page.keyboard.press('Control+Shift+n');
    await page.getByRole('button', { name: /new world/i }).first().click();
    await page.getByPlaceholder('mud://host:4000').fill('mud://safe-realm');

    // Enable SHELL restriction
    await page.getByRole('button', { name: 'Safety' }).click();
    const safetyCards = page.locator('.border.border-border.rounded-lg');
    const shellCard = safetyCards.filter({ hasText: /Disallow Shell Execution/i }).first();
    await shellCard.locator('input[type="checkbox"]').check();

    // Close to trigger sync
    await page.keyboard.press('Escape');

    await expect
      .poll(() => mockIPC.messages, { timeout: 3000 })
      .toEqual(
        expect.arrayContaining([
          expect.objectContaining({
            method: 'cmd',
            params: expect.objectContaining({ line: '/restrict SHELL' }),
          }),
        ]),
      );
  });
});

// ── World Manager — TinyFugue Script Loader ───────────────────────────────────

test.describe('World Manager — TinyFugue Script Loader', () => {
  test.beforeEach(async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();
    await page.keyboard.press('Control+Shift+n');
    await page.getByRole('button', { name: /new world/i }).first().click();
  });

  test('TinyFugue section is visible in the Scripts tab', async ({ page }) => {
    await expect(page.getByText(/TinyFugue Script/i)).toBeVisible();
  });

  test('TinyFugue file path input is visible', async ({ page }) => {
    await expect(page.getByPlaceholder('scripts/my_macros.tf')).toBeVisible();
  });

  test('Load button is visible for TinyFugue scripts', async ({ page }) => {
    // Use exact match to avoid matching "Reload" buttons for JS/Python sidecars
    const loadBtn = page.getByRole('button', { name: 'Load', exact: true });
    await expect(loadBtn).toBeVisible();
  });

  test('can fill in a TinyFugue script path', async ({ page }) => {
    const tfInput = page.getByPlaceholder('scripts/my_macros.tf');
    await tfInput.fill('tf-lib/combat.tf');
    await expect(tfInput).toHaveValue('tf-lib/combat.tf');
  });

  test('clicking Load button dispatches /load command', async ({ page, mockIPC }) => {
    const tfInput = page.getByPlaceholder('scripts/my_macros.tf');
    await tfInput.fill('tf-lib/test.tf');
    await page.getByRole('button', { name: 'Load', exact: true }).click();

    await expect
      .poll(() => mockIPC.messages, { timeout: 2000 })
      .toEqual(
        expect.arrayContaining([
          expect.objectContaining({
            method: 'cmd',
            params: expect.objectContaining({ line: '/load tf-lib/test.tf' }),
          }),
        ]),
      );
  });

  test('TF script with enabled flag dispatches /load on connect', async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();

    mockIPC.pushEvent('hook', hookConnect('tf-realm'));
    mockIPC.pushEvent('status', statusEvent('tf-realm', true));

    await page.keyboard.press('Control+Shift+n');
    await page.getByRole('button', { name: /new world/i }).first().click();
    await page.getByPlaceholder('mud://host:4000').fill('mud://tf-realm');

    await page.getByRole('button', { name: 'Scripts' }).click();

    // Enable the TF sidecar checkbox (it's the last one in the Scripts tab)
    const tfSection = page.locator('.border.border-border.rounded-lg').filter({
      hasText: /TinyFugue/i,
    });
    await tfSection.locator('input[type="checkbox"]').check();
    await page.getByPlaceholder('scripts/my_macros.tf').fill('world.tf');

    // Close to trigger sync
    await page.keyboard.press('Escape');

    await expect
      .poll(() => mockIPC.messages, { timeout: 3000 })
      .toEqual(
        expect.arrayContaining([
          expect.objectContaining({
            method: 'cmd',
            params: expect.objectContaining({ line: '/load world.tf' }),
          }),
        ]),
      );
  });
});

// ── Status Bar — Session Logging ──────────────────────────────────────────────

test.describe('Status Bar — Session Logging', () => {
  test.beforeEach(async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();
  });

  test('record button is visible in the status bar', async ({ page }) => {
    const recordBtn = page.locator('#log-record-button');
    await expect(recordBtn).toBeVisible();
  });

  test('clicking record button shows log path input', async ({ page }) => {
    await page.locator('#log-record-button').click();
    const logInput = page.locator('#log-path-input');
    await expect(logInput).toBeVisible();
  });

  test('log path input has a default timestamped filename', async ({ page }) => {
    await page.locator('#log-record-button').click();
    const logInput = page.locator('#log-path-input');
    const value = await logInput.inputValue();
    expect(value).toMatch(/^session_\d{4}-\d{2}-\d{2}_\d{6}\.log$/);
  });

  test('pressing Escape in log path input cancels without starting', async ({ page }) => {
    await page.locator('#log-record-button').click();
    await page.locator('#log-path-input').press('Escape');
    await expect(page.locator('#log-path-input')).not.toBeVisible();
  });

  test('confirming log path dispatches /log command', async ({ page, mockIPC }) => {
    await page.locator('#log-record-button').click();
    const logInput = page.locator('#log-path-input');
    await logInput.fill('session.log');
    await logInput.press('Enter');

    await expect
      .poll(() => mockIPC.messages, { timeout: 2000 })
      .toEqual(
        expect.arrayContaining([
          expect.objectContaining({
            method: 'cmd',
            params: expect.objectContaining({ line: '/log session.log' }),
          }),
        ]),
      );
  });

  test('clicking cancel button hides the log path input', async ({ page }) => {
    await page.locator('#log-record-button').click();
    await expect(page.locator('#log-path-input')).toBeVisible();
    await page.getByRole('button', { name: 'Cancel recording' }).click();
    await expect(page.locator('#log-path-input')).not.toBeVisible();
  });

  test('after starting log, clicking record button dispatches /log off', async ({
    page,
    mockIPC,
  }) => {
    // Start logging
    await page.locator('#log-record-button').click();
    await page.locator('#log-path-input').fill('test.log');
    await page.locator('#log-path-input').press('Enter');

    // Wait for /log command to be sent
    await expect
      .poll(() => mockIPC.messages, { timeout: 2000 })
      .toEqual(expect.arrayContaining([expect.objectContaining({ method: 'cmd' })]));

    // Now click the record button again to stop
    await page.locator('#log-record-button').click();

    await expect
      .poll(() => mockIPC.messages, { timeout: 2000 })
      .toEqual(
        expect.arrayContaining([
          expect.objectContaining({
            method: 'cmd',
            params: expect.objectContaining({ line: '/log off' }),
          }),
        ]),
      );
  });
});

// ── Terminal Search Panel ─────────────────────────────────────────────────────

test.describe('Terminal Search Panel', () => {
  test.beforeEach(async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();
  });

  test('Ctrl+F opens the search panel', async ({ page }) => {
    await pressCtrlF(page);
    await expect(page.getByRole('search', { name: 'Terminal search' })).toBeVisible();
  });

  test('search panel has a search input', async ({ page }) => {
    await pressCtrlF(page);
    await expect(page.getByLabel('Search query')).toBeVisible();
  });

  test('search panel shows next/prev buttons', async ({ page }) => {
    await pressCtrlF(page);
    await expect(page.getByRole('button', { name: 'Next match' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Previous match' })).toBeVisible();
  });

  test('search panel has a close button', async ({ page }) => {
    await pressCtrlF(page);
    await expect(page.getByRole('button', { name: 'Close search' })).toBeVisible();
  });

  test('Ctrl+F closes the search panel when already open', async ({ page }) => {
    await pressCtrlF(page);
    await expect(page.getByRole('search', { name: 'Terminal search' })).toBeVisible();
    await pressCtrlF(page);
    await expect(page.getByRole('search', { name: 'Terminal search' })).not.toBeVisible();
  });

  test('Escape closes the search panel', async ({ page }) => {
    await pressCtrlF(page);
    const searchPanel = page.getByRole('search', { name: 'Terminal search' });
    await expect(searchPanel).toBeVisible();
    await page.getByLabel('Search query').press('Escape');
    await expect(searchPanel).not.toBeVisible();
  });

  test('clicking close button hides the search panel', async ({ page }) => {
    await pressCtrlF(page);
    await expect(page.getByRole('search', { name: 'Terminal search' })).toBeVisible();
    await page.getByRole('button', { name: 'Close search' }).click();
    await expect(page.getByRole('search', { name: 'Terminal search' })).not.toBeVisible();
  });

  test('search input is auto-focused when panel opens', async ({ page }) => {
    await pressCtrlF(page);
    await expect(page.getByLabel('Search query')).toBeFocused();
  });

  test('shows no results message for unmatched query', async ({ page }) => {
    await pressCtrlF(page);
    await page.getByLabel('Search query').fill('xyzzy_no_match_12345');
    await expect(page.getByText('No results')).toBeVisible();
  });

  test('search panel closes after user presses close button and re-opens cleanly', async ({
    page,
  }) => {
    await pressCtrlF(page);
    await page.getByLabel('Search query').fill('test query');
    await page.getByRole('button', { name: 'Close search' }).click();

    // Re-open
    await pressCtrlF(page);
    const searchInput = page.getByLabel('Search query');
    await expect(searchInput).toBeVisible();
    await expect(searchInput).toBeFocused();
  });
});

// ── Tab Navigation — All 8 Tabs ────────────────────────────────────────────────

test.describe('World Manager — All 8 Tabs', () => {
  test('can navigate through all 8 tabs including Variables and Safety', async ({
    page,
    mockIPC,
  }) => {
    await page.goto('/');
    await mockIPC.waitConnected();
    await page.keyboard.press('Control+Shift+n');
    await page.getByRole('button', { name: /new world/i }).first().click();
, 'Aliases', 'Triggers', 'Timers', 'Keys', 'Scripts', 'Variables', 'Safety'];
    for (const tab of tabs) {
      const tabButton = page.getByRole('button', { name: tab });
      await expect(tabButton).toBeVisible();
      await tabButton.click();
    }
  });
});
