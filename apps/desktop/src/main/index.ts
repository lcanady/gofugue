import { app, BrowserWindow, ipcMain, Menu, shell, safeStorage, session } from 'electron';
import { join } from 'path';
import { readFileSync } from 'fs';
import { homedir } from 'os';
import { electronApp, optimizer, is } from '@electron-toolkit/utils';
import { spawn, execSync, type ChildProcess } from 'child_process';
import { isAllowedExternalUrl, isAllowedWebviewUrl } from './security.js';
import { CSP_PROD, CSP_DEV } from './csp.js';
import { parsePidsUnix, parsePidsWindows } from './killStale.js';

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
  // lsof is POSIX-only; on Windows there's no equivalent one-liner, so
  // skip the cleanup rather than spawning a misleading "command not
  // found" error.
  if (process.platform === 'win32') return;
  try {
    let pids: number[];

    if (process.platform === 'win32') {
      // Windows: use netstat to find PIDs on ports 7878/7879.
      const out = execSync('netstat -ano', { encoding: 'utf8' });
      pids = parsePidsWindows(out);
    } else {
      // Unix: use lsof.
      const out = execSync('lsof -ti :7878,:7879 2>/dev/null || true', { encoding: 'utf8' });
      pids = parsePidsUnix(out);
    }

    for (const pid of pids) {
      try {
        if (process.platform === 'win32') {
          execSync(`taskkill /F /PID ${pid}`);
        } else {
          process.kill(pid, 'SIGTERM');
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
  try {
    if (process.platform === 'win32') {
      execSync(`taskkill /F /T /PID ${gofugueProc.pid}`);
    } else {
      gofugueProc.kill('SIGKILL');
    }
  } catch {
    // ignore
  }
  gofugueProc = null;
}

// ── CSP via session headers (correct Electron approach) ────────────────────
//
// Setting CSP through a <meta> tag in index.html blocks Vite's HMR WebSocket
// in dev mode and can break renderer bootstrap.  session.webRequest is the
// right place: it fires for every response, lets us vary policy by env, and
// never interferes with electron-vite's dev server.

function installCSP(): void {
  // Always install a CSP. Use the stricter prod policy in production and a
  // slightly more permissive dev policy that still protects the renderer while
  // allowing Vite HMR / React Fast Refresh to function.
  const policy = is.dev ? CSP_DEV : CSP_PROD;

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

  // ── H-2: webview origin guard ─────────────────────────────────────────────
  // Reject any <webview> whose src is not https:// or file://, and enforce
  // that node integration is disabled and context isolation is on — regardless
  // of what the renderer requests.
  win.webContents.on('will-attach-webview', (event, webPreferences, params) => {
    // Validate src scheme.
    const src: string = (params as { src?: string }).src ?? '';
    if (!isAllowedWebviewUrl(src)) {
      event.preventDefault();
      return;
    }
    // Harden webPreferences regardless of renderer-supplied values.
    webPreferences.nodeIntegration = false;
    webPreferences.contextIsolation = true;
  });

  win.webContents.setWindowOpenHandler(({ url }) => {
    if (isAllowedExternalUrl(url)) {
      shell.openExternal(url);
    }
    return { action: 'deny' };
  });

  const windowId = win.id;
  if (is.dev && process.env.ELECTRON_RENDERER_URL) {
    win.loadURL(`${process.env.ELECTRON_RENDERER_URL}?windowId=${windowId}`);
  } else {
    win.loadFile(join(__dirname, '../renderer/index.html'), {
      query: { windowId: String(windowId) },
    });
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

ipcMain.handle('window:new', () => {
  createWindow();
  return { ok: true };
});

// ── IPC token — read the shared secret written by gofugue on startup ─────────
//
// gofugue writes ~/.config/gofugue/ipc.token (mode 0600) when it starts.
// The renderer cannot read the filesystem directly, so the main process reads
// it here and returns it via contextBridge.

ipcMain.handle('gofugue:getToken', (): string => {
  try {
    const tokenPath = join(homedir(), '.config', 'gofugue', 'ipc.token');
    return readFileSync(tokenPath, 'utf8').trim();
  } catch {
    return '';
  }
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
        label: 'File',
        submenu: [
          {
            label: 'New Window',
            accelerator: 'CmdOrCtrl+N',
            click: (): void => {
              createWindow();
            },
          },
          { type: 'separator' },
          { role: 'close' },
        ],
      },
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
  app.quit();
  try {
    process.kill(0, 'SIGINT');
  } catch {
    process.exit(0);
  }
});

app.on('before-quit', () => {
  stopGofugue();
  try {
    process.kill(0, 'SIGINT');
  } catch {
    process.exit(0);
  }
});
