/// <reference types="vite/client" />

/** Exposed by the Electron preload script via contextBridge. */
interface Window {
  electron?: {
    process?: {
      platform: string;
    };
  };
  gofugue?: {
    start: () => Promise<{ ok: boolean }>;
    stop: () => Promise<{ ok: boolean }>;
    status: () => Promise<{ running: boolean }>;
    encryptPassword: (plain: string) => Promise<string>;
    decryptPassword: (b64: string) => Promise<string>;
    quit: () => Promise<void>;
    newWindow: () => Promise<{ ok: boolean }>;
  };
}

// Allow <webview> JSX in Electron renderer.
declare namespace JSX {
  interface IntrinsicElements {
    webview: React.DetailedHTMLProps<
      React.HTMLAttributes<HTMLElement> & {
        src?: string;
        preload?: string;
        partition?: string;
        allowpopups?: string;
        webpreferences?: string;
        style?: React.CSSProperties;
      },
      HTMLElement
    >;
  }
}
