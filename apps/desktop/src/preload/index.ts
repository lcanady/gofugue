import { contextBridge, ipcRenderer } from 'electron';
import { electronAPI } from '@electron-toolkit/preload';

// Expose gofugue process control to the renderer.
const gofugueAPI = {
  start: () => ipcRenderer.invoke('gofugue:start') as Promise<{ ok: boolean }>,
  stop: () => ipcRenderer.invoke('gofugue:stop') as Promise<{ ok: boolean }>,
  status: () => ipcRenderer.invoke('gofugue:status') as Promise<{ running: boolean }>,
  /** Encrypt a password string using OS safeStorage. Returns base64 or '' if unavailable. */
  encryptPassword: (plain: string) =>
    ipcRenderer.invoke('profile:encryptPassword', plain) as Promise<string>,
  /** Decrypt a base64 safeStorage blob. Returns plaintext or '' on failure. */
  decryptPassword: (b64: string) =>
    ipcRenderer.invoke('profile:decryptPassword', b64) as Promise<string>,
  /** Quit the Electron app (stop gofugue + close all windows). */
  quit: () => ipcRenderer.invoke('app:quit') as Promise<void>,
  /** Open a new window. */
  newWindow: () => ipcRenderer.invoke('window:new') as Promise<{ ok: boolean }>,
  /**
   * Read the shared IPC auth token that gofugue writes to
   * ~/.config/gofugue/ipc.token on startup. Returns empty string if the file
   * is not yet available (e.g. gofugue not yet started).
   */
  getIPCToken: () => ipcRenderer.invoke('gofugue:getToken') as Promise<string>,
};

if (process.contextIsolated) {
  try {
    contextBridge.exposeInMainWorld('electron', electronAPI);
    contextBridge.exposeInMainWorld('gofugue', gofugueAPI);
  } catch (e) {
    console.error(e);
  }
} else {
  // @ts-expect-error (define in global only for non-contextIsolated)
  window.electron = electronAPI;
  // @ts-expect-error
  window.gofugue = gofugueAPI;
}
