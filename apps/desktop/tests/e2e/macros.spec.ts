import { test, expect } from './fixtures/mock-ipc.js';
import { hookConnect, statusEvent } from './helpers/ipc-events.js';

test.describe('World Manager — Macro Editors', () => {
  test.beforeEach(async ({ page, mockIPC }) => {
    page.on('console', (msg) => {
      console.log(`[BROWSER CONSOLE] [${msg.type()}] ${msg.text()}`);
    });
    page.on('pageerror', (err) => {
      console.error(`[BROWSER ERROR] ${err.stack || err.message}`);
    });

    await page.goto('/');
    await mockIPC.waitConnected();
  });

  test('can navigate through tabs in the World Manager Dialog', async ({ page }) => {
    // Open Dialog
    await page.keyboard.press('Control+Shift+n');
    const dialog = page.getByRole('dialog', { name: 'Worlds' });
    await expect(dialog).toBeVisible({ timeout: 2000 });

    // Add a new world connection profile
    await page.getByRole('button', { name: /new world/i }).click();

    // Verify Tab Headers exist and click them
    const tabs = ['General', 'Aliases', 'Triggers', 'Timers', 'Keys', 'Scripts', 'Variables', 'Safety'];
    for (const tab of tabs) {
      const tabButton = page.getByRole('button', { name: tab });
      await expect(tabButton).toBeVisible();
      await tabButton.click();
    }
  });

  test('can add, edit, and delete an alias', async ({ page }) => {
    await page.keyboard.press('Control+Shift+n');
    await page.getByRole('button', { name: /new world/i }).click();

    // Switch to Aliases tab
    await page.getByRole('button', { name: 'Aliases' }).click();
    await expect(page.getByRole('button', { name: 'Add Alias' })).toBeVisible();

    // Add Alias
    await page.getByRole('button', { name: 'Add Alias' }).click();
    await expect(page.getByPlaceholder('e.g. kk')).toBeVisible();

    // Fill fields
    await page.getByPlaceholder('e.g. kk').fill('hello');
    await page.getByPlaceholder('e.g. kill target').fill('say hello world');

    // Delete Alias
    await page.locator('.text-muted-foreground.hover\\:text-destructive').click();
    await expect(page.getByText('No aliases defined yet.')).toBeVisible();
  });

  test('can add, edit, and delete a trigger', async ({ page }) => {
    await page.keyboard.press('Control+Shift+n');
    await page.getByRole('button', { name: /new world/i }).click();

    // Switch to Triggers tab
    await page.getByRole('button', { name: 'Triggers' }).click();
    await expect(page.getByRole('button', { name: 'Add Trigger' })).toBeVisible();

    // Add Trigger
    await page.getByRole('button', { name: 'Add Trigger' }).click();
    await expect(page.getByPlaceholder('e.g. ^You feel better')).toBeVisible();

    // Fill fields
    await page.getByPlaceholder('e.g. ^You feel better').fill('a fierce dragon');
    await page.getByPlaceholder('e.g. stand; /echo healed!').fill('cast fireball');

    // Change match mode (value is 'substr' for Literal)
    const modeSelect = page.locator('select').first();
    await modeSelect.selectOption('substr');

    // Change trigger type
    const typeSelect = page.locator('select').nth(1);
    await typeSelect.selectOption('gag');

    // Delete Trigger
    await page.locator('.text-muted-foreground.hover\\:text-destructive').click();
    await expect(page.getByText('No triggers defined yet.')).toBeVisible();
  });

  test('can add, edit, and delete a timer', async ({ page }) => {
    await page.keyboard.press('Control+Shift+n');
    await page.getByRole('button', { name: /new world/i }).click();

    // Switch to Timers tab
    await page.getByRole('button', { name: 'Timers' }).click();
    await expect(page.getByRole('button', { name: 'Add Timer' })).toBeVisible();

    // Add Timer
    await page.getByRole('button', { name: 'Add Timer' }).click();
    await expect(page.getByPlaceholder('e.g. 5s or 1m')).toBeVisible();

    // Fill fields
    await page.getByPlaceholder('e.g. 5s or 1m').fill('5s');
    await page.getByPlaceholder('e.g. look or /say alive!').fill('look');

    // Delete Timer
    await page.locator('.text-muted-foreground.hover\\:text-destructive').click();
    await expect(page.getByText('No timers defined yet.')).toBeVisible();
  });

  test('can add, edit, and record key combinations for keybindings', async ({ page }) => {
    await page.keyboard.press('Control+Shift+n');
    await page.getByRole('button', { name: /new world/i }).click();

    // Switch to Keys tab
    await page.getByRole('button', { name: 'Keys' }).click();
    await expect(page.getByRole('button', { name: 'Add Keybinding' })).toBeVisible();

    // Add Keybinding
    await page.getByRole('button', { name: 'Add Keybinding' }).click();
    const shortcutInput = page.getByPlaceholder('Focus & press key combo');
    await expect(shortcutInput).toBeVisible();

    // Focus & record key combo
    await shortcutInput.focus();
    await page.keyboard.press('F1');
    await expect(shortcutInput).toHaveValue('f1');

    await page.getByPlaceholder('e.g. north or /say run!').fill('cast heal');

    // Delete Keybinding
    await page.locator('.text-muted-foreground.hover\\:text-destructive').click();
    await expect(page.getByText('No keybindings defined yet.')).toBeVisible();
  });

  test('can configure script sidecars and view console', async ({ page }) => {
    await page.keyboard.press('Control+Shift+n');
    await page.getByRole('button', { name: /new world/i }).click();

    // Switch to Scripts tab
    const scriptsTab = page.getByRole('button', { name: 'Scripts' });
    await expect(scriptsTab).toBeVisible();
    await scriptsTab.click();
    await expect(page.getByText(/Scripting Sidecar/i)).toBeVisible();

    // Edit JS path
    const jsPath = page.getByPlaceholder('scripts/my_script.js');
    await jsPath.fill('scripts/combat.js');

    // Edit Py path
    const pyPath = page.getByPlaceholder('scripts/my_script.py');
    await pyPath.fill('scripts/combat.py');
  });

  test('can manage character profiles under a world connection profile', async ({ page }) => {
    await page.keyboard.press('Control+Shift+n');
    await page.getByRole('button', { name: /new world/i }).click();

    // Add character
    const addCharBtn = page.getByRole('button', { name: 'Add character' });
    await addCharBtn.click();

    // Fill character fields
    await page.getByPlaceholder('PlayerName').fill('Gandalf');
    await page.getByPlaceholder('connect {name} {password}\nother setup command').fill('connect Gandalf 12345');

    // Select the world again in sidebar
    await page.getByRole('button', { name: 'New World' }).first().click();
  });
});

test.describe('World Manager — Live Sync & Keybinding triggers', () => {
  test('pressing active world keybinding dispatches command over WebSocket', async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();

    // Connect to a world named 'mystic'
    mockIPC.pushEvent('hook', hookConnect('mystic'));
    mockIPC.pushEvent('status', statusEvent('mystic', true));

    // Open worlds dialog and configure F2 keybinding for 'mystic'
    await page.keyboard.press('Control+Shift+n');
    await page.getByRole('button', { name: /new world/i }).click();
    await page.getByPlaceholder('My MUD').fill('mystic');
    await page.getByPlaceholder('mud://host:4000').fill('mud://mystic');
    await page.getByRole('button', { name: 'Keys' }).click();
    await page.getByRole('button', { name: 'Add Keybinding' }).click();

    const shortcutInput = page.getByPlaceholder('Focus & press key combo');
    await shortcutInput.focus();
    await page.keyboard.press('F2');
    await page.getByPlaceholder('e.g. north or /say run!').fill('/say f2 pressed');

    // Close dialog to trigger macro synchronization
    await page.keyboard.press('Escape');

    // Press F2 globally and verify that the command dispatches over the WebSocket
    await page.keyboard.press('F2');

    await expect
      .poll(() => mockIPC.messages, { timeout: 2000 })
      .toEqual(
        expect.arrayContaining([
          expect.objectContaining({
            method: 'cmd',
            params: { line: '/say f2 pressed', world: 'mystic' },
          }),
        ]),
      );
  });
});
