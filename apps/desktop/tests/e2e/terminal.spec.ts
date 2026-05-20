/**
 * Terminal pane rendering tests.
 *
 * Verifies that world.line.rendered events are displayed, gagged lines are
 * suppressed, ANSI colour spans render with inline styles, and accessibility
 * attributes are present.
 */
import { test, expect } from './fixtures/mock-ipc.js';
import { line, colouredLine, gaggedLine, hookConnect } from './helpers/ipc-events.js';

async function setupWorld(mockIPC: Awaited<ReturnType<(typeof test)['extend']>> extends never ? never : Parameters<Parameters<typeof test>[0]>[0]['mockIPC'], worldName: string) {
  mockIPC.pushEvent('hook', hookConnect(worldName));
}

test.describe('Terminal pane — line rendering', () => {
  test.beforeEach(async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();
    mockIPC.pushEvent('hook', hookConnect('testworld'));
    // Give React time to create the world tab and switch the active world
    await page.getByRole('tab', { name: /testworld/ }).waitFor({ timeout: 2_000 });
  });

  test('terminal pane has aria-live=polite for screen reader support', async ({ page }) => {
    await expect(page.locator('[aria-live="polite"][aria-label]')).toBeVisible();
  });

  test('a plain text line appears in the terminal', async ({ page, mockIPC }) => {
    mockIPC.pushEvent('world.line.rendered', line('The weather is fine today.', 'testworld'));
    await expect(page.getByText('The weather is fine today.')).toBeVisible({ timeout: 2_000 });
  });

  test('multiple lines appear in insertion order', async ({ page, mockIPC }) => {
    mockIPC.pushEvent('world.line.rendered', line('Line one', 'testworld'));
    mockIPC.pushEvent('world.line.rendered', line('Line two', 'testworld'));
    mockIPC.pushEvent('world.line.rendered', line('Line three', 'testworld'));

    await expect(page.getByText('Line one')).toBeVisible({ timeout: 2_000 });
    await expect(page.getByText('Line two')).toBeVisible();
    await expect(page.getByText('Line three')).toBeVisible();
  });

  test('gagged lines are not rendered', async ({ page, mockIPC }) => {
    mockIPC.pushEvent('world.line.rendered', gaggedLine('testworld'));
    // A sentinel non-gagged line to confirm events are flowing
    mockIPC.pushEvent('world.line.rendered', line('sentinel line', 'testworld'));

    await expect(page.getByText('sentinel line')).toBeVisible({ timeout: 2_000 });
    await expect(page.getByText('this line should not appear')).not.toBeVisible();
  });

  test('ANSI colour line renders coloured spans', async ({ page, mockIPC }) => {
    mockIPC.pushEvent(
      'world.line.rendered',
      colouredLine('testworld', [
        { text: 'You are ', fg: -1 },
        { text: 'hungry', fg: 1 }, // red (palette index 1)
        { text: '.', fg: -1 },
      ]),
    );

    await expect(page.getByText('You are')).toBeVisible({ timeout: 2_000 });
    // The coloured "hungry" span should have a style with the red ANSI colour
    const hungrySpan = page.locator('span').filter({ hasText: /^hungry$/ });
    await expect(hungrySpan).toBeVisible({ timeout: 2_000 });
    // Palette index 1 = #cd3131
    await expect(hungrySpan).toHaveCSS('color', 'rgb(205, 49, 49)');
  });

  test('bold ANSI span renders with font-weight bold', async ({ page, mockIPC }) => {
    mockIPC.pushEvent(
      'world.line.rendered',
      colouredLine('testworld', [
        { text: 'IMPORTANT', fg: -1, bold: true },
      ]),
    );

    const span = page.locator('span').filter({ hasText: /^IMPORTANT$/ });
    await expect(span).toBeVisible({ timeout: 2_000 });
    await expect(span).toHaveCSS('font-weight', '700');
  });

  test('lines from other worlds are not shown in the active world terminal', async ({
    page,
    mockIPC,
  }) => {
    mockIPC.pushEvent('hook', hookConnect('otherworld'));
    mockIPC.pushEvent(
      'world.line.rendered',
      line('secret from otherworld', 'otherworld'),
    );
    const sentinel = line('sentinel in testworld', 'testworld');
    mockIPC.pushEvent('world.line.rendered', sentinel);

    await expect(page.getByText('sentinel in testworld')).toBeVisible({ timeout: 2_000 });
    await expect(page.getByText('secret from otherworld')).not.toBeVisible();
  });
});

test.describe('Terminal pane — local echo (warnings)', () => {
  test('local world lines appear in the terminal', async ({ page, mockIPC }) => {
    await page.goto('/');
    await mockIPC.waitConnected();
    mockIPC.pushEvent('hook', hookConnect('local'));
    await page.getByRole('tab', { name: /local/ }).waitFor({ timeout: 2_000 });

    mockIPC.pushEvent('world.line.rendered', line('[WARNING] TLS disabled', 'local'));
    await expect(page.getByText('[WARNING] TLS disabled')).toBeVisible({ timeout: 2_000 });
  });
});
