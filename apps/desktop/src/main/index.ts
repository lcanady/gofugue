import { app, BrowserWindow, ipcMain, Menu, shell, safeStorage, session } from 'electron';
import { join } from 'path';
import { electronApp, optimizer, is } from '@electron-toolkit/utils';
import { spawn, execSync, type ChildProcess } from 'child_process';
import { isAllowedExternalUrl } from './security.js';

// ── gofugue process management ─────────────────────────────────────────────

let gofugueProc: ChildProcess | null = null;

/**
 * Resolve the gofugue binary path for the current environment.
 *
 * Dev:  <project-root>/bin/gf  — built by `pnpm dev` (go build -o bin/gf ./cmd/gf).
 *       Override with the GF_BINARY env var for custom setups.
 * Prod: Electron's resourcesPath, where electron-builder bundles the binary.
 */
function gofugueBinaryPath(): string {
  if (is.dev) {
    // app.getAppPath() → apps/desktop/  →  ../../  → project root
    return process.env['GF_BINARY'] ?? join(app.getAppPath(), '../../bin/gf');
  }
  return join(process.resourcesPath, 'gf');
}

/**
 * C3: Kill any stale gofugue holding our ports before spawning a fresh one.
 * Without this, a leftover process from a previous session occupies port 7879
 * and the new binary exits silently — the frontend then connects to the stale
 * process, which already has worlds connected (causing phantom tabs on startup).
 */
function killStaleGofugue(): void {
  try {
    const pids: string[] = [];

    if (process.platform === 'win32') {
      // Windows: use netstat to find PIDs on ports 7878/7879.
      const out = execSync('netstat -ano', { encoding: 'utf8' });
      const lines = out.split('\n');
      for (const line of lines) {
        if (line.includes(':7878') || line.includes(':7879')) {
          const parts = line.trim().split(/\s+/);
          const pid = parts[parts.length - 1];
          if (pid && pid !== '0' && !pids.includes(pid)) {
            pids.push(pid);
          }
        }
      }
    } else {
      // Unix: use lsof.
      execSync('lsof -ti :7878,:7879 2>/dev/null || true', { encoding: 'utf8' })
        .split('\n')
        .map((s) => s.trim())
        .filter(Boolean)
        .forEach((pid) => pids.push(pid));
    }

    for (const pid of pids) {
      try {
        if (process.platform === 'win32') {
          execSync(`taskkill /F /PID ${pid}`);
        } else {
          process.kill(Number(pid), 'SIGTERM');
        }
      } catch {
        /* already gone or access denied */
      }
    }

    if (pids.length > 0) {
      console.log(`[gofugue] killed ${pids.length} stale process(es) on ports 7878/7879`);
    }
  } catch {
    // tools not available or no stale processes — proceed normally.
  }
}

function startGofugue(): void {
  if (gofugueProc) return;

  killStaleGofugue();

  const binaryPath = gofugueBinaryPath();

  gofugueProc = spawn(binaryPath, ['--headless'], {
    stdio: ['ignore', 'pipe', 'pipe'],
    detached: false,
  });

  gofugueProc.stdout?.on('data', (d: Buffer) =>
    console.log('[gofugue]', d.toString().trimEnd()),
  );
  gofugueProc.stderr?.on('data', (d: Buffer) =>
    console.error('[gofugue]', d.toString().trimEnd()),
  );

  gofugueProc.on('exit', (code, signal) => {
    console.log(`gofugue exited code=${code} signal=${signal}`);
    gofugueProc = null;
  });

  gofugueProc.on('error', (err) => {
    console.error('Failed to start gofugue:', err.message);
    gofugueProc = null;
  });
}

function stopGofugue(): void {
  if (!gofugueProc) return;
  gofugueProc.kill();
  gofugueProc = null;
}

// ── CSP via session headers (correct Electron approach) ────────────────────
//
// Setting CSP through a <meta> tag in index.html blocks Vite's HMR WebSocket
// in dev mode and can break renderer bootstrap.  session.webRequest is the
// right place: it fires for every response, lets us vary policy by env, and
// never interferes with electron-vite's dev server.

function installCSP(): void {
  // Dev: Vite injects inline scripts for HMR + React Fast Refresh, and opens
  // its own WebSocket on localhost.  Any CSP breaks that, so skip it entirely.
  // Prod: enforce a strict policy via response headers (the right Electron way —
  // <meta> tags interfere with the renderer bootstrap).
  if (is.dev) return;

  const policy = [
    "default-src 'self'",
    "script-src 'self'",
    "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
    "font-src 'self' https://fonts.gstatic.com",
    "connect-src ws://127.0.0.1:7879 wss://127.0.0.1:7879",
    "img-src 'self' data:",
  ].join('; ');

  session.defaultSession.webRequest.onHeadersReceived((details, callback) => {
    callback({
      responseHeaders: {
        ...details.responseHeaders,
        'Content-Security-Policy': [policy],
      },
    });
  });
}

// ── Window ─────────────────────────────────────────────────────────────────

function createWindow(): BrowserWindow {
  const win = new BrowserWindow({
    width: 1280,
    height: 800,
    minWidth: 640,
    minHeight: 400,
    show: false,
    autoHideMenuBar: true,
    titleBarStyle: process.platform === 'darwin' ? 'hiddenInset' : 'default',
    backgroundColor: '#080d1a', // matches --background token
    webPreferences: {
      preload: join(__dirname, '../preload/index.cjs'),
      sandbox: true,
      contextIsolation: true,
      webviewTag: true,
    },
  });

  win.on('ready-to-show', () => win.show());

  win.webContents.setWindowOpenHandler(({ url }) => {
    if (isAllowedExternalUrl(url)) {
      shell.openExternal(url);
    }
    return { action: 'deny' };
  });

  if (is.dev && process.env.ELECTRON_RENDERER_URL) {
    win.loadURL(process.env.ELECTRON_RENDERER_URL);
  } else {
    win.loadFile(join(__dirname, '../renderer/index.html'));
  }

  return win;
}

// ── IPC handlers ───────────────────────────────────────────────────────────

ipcMain.handle('gofugue:start', () => {
  startGofugue();
  return { ok: true };
});

ipcMain.handle('gofugue:stop', () => {
  stopGofugue();
  return { ok: true };
});

ipcMain.handle('gofugue:status', () => ({
  running: gofugueProc !== null,
}));

ipcMain.handle('app:quit', () => {
  app.quit();
});

// ── safeStorage IPC — password encryption ──────────────────────────────────

ipcMain.handle('profile:encryptPassword', (_event, plain: string): string => {
  if (!safeStorage.isEncryptionAvailable()) return '';
  return safeStorage.encryptString(plain).toString('base64');
});

ipcMain.handle('profile:decryptPassword', (_event, b64: string): string => {
  if (!safeStorage.isEncryptionAvailable() || !b64) return '';
  try {
    return safeStorage.decryptString(Buffer.from(b64, 'base64'));
  } catch {
    return '';
  }
});

// ── App lifecycle ──────────────────────────────────────────────────────────

app.whenReady().then(() => {
  electronApp.setAppUserModelId('dev.kumakun.gofugue');

  app.on('browser-window-created', (_, window) => {
    optimizer.watchWindowShortcuts(window);
  });

  // Install CSP response-header policy before any window opens.
  installCSP();

  // Start the gofugue backend immediately.
  startGofugue();

  createWindow();

  // Native menu: minimal set (macOS needs at least app + edit for shortcuts).
  Menu.setApplicationMenu(
    Menu.buildFromTemplate([
      ...(process.platform === 'darwin'
        ? [
            {
              label: app.name,
              submenu: [
                { role: 'about' as const },
                { type: 'separator' as const },
                { role: 'services' as const },
                { type: 'separator' as const },
                { role: 'hide' as const },
                { role: 'hideOthers' as const },
                { role: 'unhide' as const },
                { type: 'separator' as const },
                { role: 'quit' as const },
              ],
            },
          ]
        : []),
      {
        label: 'Edit',
        submenu: [
          { role: 'undo' },
          { role: 'redo' },
          { type: 'separator' },
          { role: 'cut' },
          { role: 'copy' },
          { role: 'paste' },
          { role: 'selectAll' },
        ],
      },
      {
        label: 'View',
        submenu: [
          { role: 'reload' },
          { role: 'forceReload' },
          { role: 'toggleDevTools' },
          { type: 'separator' },
          { role: 'togglefullscreen' },
        ],
      },
    ]),
  );

  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) createWindow();
  });
});

app.on('window-all-closed', () => {
  stopGofugue();
  if (process.platform !== 'darwin') app.quit();
});

app.on('before-quit', stopGofugue);
