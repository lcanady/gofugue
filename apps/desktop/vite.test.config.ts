/**
 * Standalone Vite config for E2E testing.
 *
 * Serves the renderer as a plain browser app — no Electron main or preload
 * involved. workspace packages are resolved to their TypeScript source so no
 * pre-build step is needed.
 */
import { resolve } from 'path';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

const root = resolve(__dirname, 'src/renderer');

export default defineConfig({
  root,
  plugins: [react()],
  resolve: {
    alias: {
      '@renderer': resolve(__dirname, 'src/renderer/src'),
      // Point directly at TS source — Vite handles transpilation, no dist needed.
      '@gofugue/ipc': resolve(__dirname, '../../packages/ipc/src/index.ts'),
      '@gofugue/ansi-renderer': resolve(
        __dirname,
        '../../packages/ansi-renderer/src/index.ts',
      ),
    },
  },
  server: {
    port: 5174, // different from the electron-vite dev port (5173)
  },
  // Suppress "process is not defined" and Electron-specific globals in browser.
  define: {
    'process.env': '{}',
  },
});
