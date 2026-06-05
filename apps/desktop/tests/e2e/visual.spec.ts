/**
 * Visual regression & design-sanity tests.
 *
 * Two layers:
 *   1. Computed-style assertions — verify design tokens (colours, fonts,
 *      layout) are applied correctly without pixel-snapping brittleness.
 *   2. Screenshot snapshots — catch unexpected visual regressions. Run
 *      `pnpm playwright test --update-snapshots` to refresh baselines.
 */
import { test, expect } from './fixtures/mock-ipc.js';
import { line, hookConnect, statusEvent } from './helpers/ipc-events.js';

// ---------------------------------------------------------------------------
// Design token assertions
// ---------------------------------------------------------------------------

test.describe('Design tokens — colours', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
  });

  test('root background uses the dark background token', async ({ page }) => {
    // --background: 240 6% 6% → hsl(240, 6%, 6%) ≈ #0e0e10
    const bg = await page.evaluate(() =>
      getComputedStyle(document.body).backgroundColor,
    );
    // Expect rgb(14, 14, 16)
    expect(bg).toBe('rgb(14, 14, 16)');
  });

  test('primary accent is an indigo colour (--primary)', async ({ page }) => {
    // --primary: 239 84% 67% → indigo
    const primary = await page.evaluate(() =>
      getComputedStyle(document.documentElement).getPropertyValue('--primary').trim(),
    );
    expect(primary).toMatch(/^239/);
  });

  test('terminal cursor CSS variable is set', async ({ page }) => {
    const cursor = await page.evaluate(() =>
      getComputedStyle(document.documentElement)
        .getPropertyValue('--terminal-cursor')
        .trim(),
    );
    expect(cursor).not.toBe('');
    expect(cursor).toMatch(/239/);
  });

  test('all 16 ANSI palette variables are defined', async ({ page }) => {
    const missing = await page.evaluate(() => {
      const styles = getComputedStyle(document.documentElement);
      return Array.from({ length: 16 }, (_, i) => `--ansi-${i}`)
        .filter((v) => !styles.getPropertyValue(v).trim());
    });
    expect(missing).toHaveLength(0);
  });

  test('ANSI red (index 1) is #cd3131', async ({ page }) => {
    const ansi1 = await page.evaluate(() =>
      getComputedStyle(document.documentElement).getPropertyValue('--ansi-1').trim(),
    );
    expect(ansi1.toLowerCase()).toBe('#cd3131');
  });
});

test.describe('Design tokens — typography', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/');
  });

  test('input bar uses a monospace font', async ({ page }) => {
    const fontFamily = await page
      .getByRole('textbox', { name: 'Command input' })
      .evaluate((el) => getComputedStyle(el).fontFamily);
    // Should include JetBrains Mono or fall back to a monospace family
    expect(fontFamily.toLowerCase()).toMatch(/mono|courier|consolas/);
  });

  test('status bar uses a monospace font', async ({ page }) => {
    const fontFamily = await page
      .getByRole('status')
      .evaluate((el) => getComputedStyle(el).fontFamily);
    expect(fontFamily.toLowerCase()).toMatch(/mono|courier|consolas/);
  });
});

test.describe('Design tokens — layout geometry', () => {
  test.beforeEach(async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();
    mockIPC.pushEvent('hook', hookConnect('avalon'));
    await page.locator('.lm_tab', { hasText: 'avalon' }).waitFor({ timeout: 2_000 });
  });

  test('tab bar is at the top of the window', async ({ page }) => {
    const tabBar = page.locator('.lm_header');
    const box = await tabBar.boundingBox();
    // Top edge should be at y=0 (or very close, accounting for possible macOS drag region)
    expect(box!.y).toBeLessThan(40);
  });

  test('status bar is at the bottom of the window', async ({ page }) => {
    const statusBar = page.getByRole('status');
    const windowHeight = await page.evaluate(() => window.innerHeight);
    const box = await statusBar.boundingBox();
    // Bottom edge of status bar should be at (or very near) the window bottom
    expect(box!.y + box!.height).toBeGreaterThan(windowHeight - 10);
  });

  test('input bar is directly above the status bar', async ({ page }) => {
    const inputForm = page.locator('form').first();
    const statusBar = page.getByRole('status');
    const inputBox = await inputForm.boundingBox();
    const statusBox = await statusBar.boundingBox();
    // Input form bottom ≈ status bar top
    expect(Math.abs(inputBox!.y + inputBox!.height - statusBox!.y)).toBeLessThan(4);
  });

  test('world tab bar height is ~36px', async ({ page }) => {
    const box = await page.locator('.lm_header').boundingBox();
    expect(box!.height).toBeGreaterThanOrEqual(32);
    expect(box!.height).toBeLessThanOrEqual(42);
  });

  test('status bar height is ~24px', async ({ page }) => {
    const box = await page.getByRole('status').boundingBox();
    expect(box!.height).toBeGreaterThanOrEqual(20);
    expect(box!.height).toBeLessThanOrEqual(30);
  });
});

// ---------------------------------------------------------------------------
// Screenshot-based visual regression (snapshots auto-created on first run)
// ---------------------------------------------------------------------------

test.describe('Visual snapshots', () => {
  test('disconnected empty state', async ({ page }) => {
    await page.routeWebSocket('ws://127.0.0.1:7879/', (ws) => {
      ws.close();
    });
    await page.goto('/');
    // Wait for the status bar connecting indicator to be visible
    await page.getByRole('status').getByText(/connecting/i).waitFor({ timeout: 3_000 });
    await expect(page).toHaveScreenshot('disconnected-empty-state.png', {
      maxDiffPixelRatio: 0.02,
    });
  });

  test('connected no-world state', async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();
    // IPC is up but no worlds connected yet
    await page.getByText(/no worlds connected/i).waitFor({ timeout: 3_000 });
    await expect(page).toHaveScreenshot('connected-no-world.png', {
      maxDiffPixelRatio: 0.02,
    });
  });

  test('terminal with output lines', async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();

    mockIPC.pushEvent('hook', hookConnect('avalon'));
    mockIPC.pushEvent('status', statusEvent('avalon', true, 12));
    await page.locator('.lm_tab', { hasText: 'avalon' }).waitFor({ timeout: 2_000 });

    // Push some representative output
    const lines = [
      'Avalon: The Legend Lives',
      '─────────────────────────────────────',
      'You are standing in the town square.',
      'A fountain burbles quietly to the north.',
      '',
      'Obvious exits: north, east, south, west.',
    ];
    for (const text of lines) {
      mockIPC.pushEvent('world.line.rendered', line(text, 'avalon'));
    }
    await page.getByText('Obvious exits:').waitFor({ timeout: 2_000 });

    await expect(page).toHaveScreenshot('terminal-with-output.png', {
      maxDiffPixelRatio: 0.02,
    });
  });

  test('world manager dialog', async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();
    await page.keyboard.press('Control+Shift+n');
    await page.getByRole('dialog', { name: 'Worlds' }).waitFor({ timeout: 2_000 });
    await expect(page).toHaveScreenshot('world-manager-dialog.png', {
      maxDiffPixelRatio: 0.02,
    });
  });
});
