/**
 * GoldenLayoutRoot — replaces the custom split-tree with GoldenLayout v2.
 *
 * GoldenLayout owns all drag/drop/resize/tab-stacking behaviour.
 * React components are injected into GL's managed DOM containers via portals.
 *
 * Panel types:
 *   terminal  – TerminalPane for a connected world
 *   spawn     – Filtered world output (stub)
 *   webview   – Chromium <webview>
 */
import { useEffect, useRef, useState, useCallback } from 'react';
import { createPortal } from 'react-dom';
import {
  GoldenLayout,
  type LayoutConfig,
  type ComponentContainer,
  ItemType,
} from 'golden-layout';
import { cn } from '@renderer/lib/utils';
import { useLayoutStore } from '@renderer/store/glLayout';
import { TerminalPanel } from './panels/TerminalPanel';
import { SpawnPanel } from './panels/SpawnPanel';
import { WebviewPanel } from './panels/WebviewPanel';
import type { GofugueClient } from '@gofugue/ipc';

// ── Types ─────────────────────────────────────────────────────────────────────

export type PanelState =
  | { kind: 'terminal'; worldName: string }
  | { kind: 'spawn'; worldName: string; filter: string; label: string }
  | { kind: 'webview'; url: string; label: string };

interface Portal {
  id: string;
  element: React.ReactElement;
  target: Element;
}

// ── Default layout ────────────────────────────────────────────────────────────

const EMPTY_CONFIG: LayoutConfig = {
  root: {
    type: ItemType.stack,
    content: [],
  },
  header: {
    popout: false,
    maximise: false,
  },
};

// ── Component ──────────────────────────────────────────────────────────────────

interface Props {
  client: GofugueClient;
  onNewConnection: () => void;
  className?: string;
}

export function GoldenLayoutRoot({ client, onNewConnection, className }: Props) {
  const containerRef = useRef<HTMLDivElement>(null);
  const glRef = useRef<GoldenLayout | null>(null);
  const [portals, setPortals] = useState<Portal[]>([]);
  const [terminalCount, setTerminalCount] = useState(0);
  // Keep a stable ref so portal callbacks don't close over stale state
  const portalsRef = useRef<Portal[]>([]);
  const terminalCountRef = useRef(0);

  const { saveConfig, loadConfig, setGl } = useLayoutStore();

  // ── Portal management ────────────────────────────────────────────────────────

  const addPortal = useCallback((id: string, element: React.ReactElement, target: Element, isTerminal = false) => {
    const portal: Portal = { id, element, target };
    portalsRef.current = [...portalsRef.current, portal];
    setPortals([...portalsRef.current]);
    if (isTerminal) {
      terminalCountRef.current += 1;
      setTerminalCount(terminalCountRef.current);
    }
  }, []);

  const removePortal = useCallback((id: string, isTerminal = false) => {
    portalsRef.current = portalsRef.current.filter((p) => p.id !== id);
    setPortals([...portalsRef.current]);
    if (isTerminal) {
      terminalCountRef.current = Math.max(0, terminalCountRef.current - 1);
      setTerminalCount(terminalCountRef.current);
    }
  }, []);

  // ── GoldenLayout init ────────────────────────────────────────────────────────

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;

    const gl = new GoldenLayout(el);
    glRef.current = gl;
    setGl(gl);

    gl.on('stackCreated' as never, (stack: any) => {
      const header = stack.header;
      if (!header) return;

      const tabsContainer = header.tabsContainer;
      if (!tabsContainer) return;

      let plusBtn = tabsContainer.querySelector('.lm_plus_button');
      if (!plusBtn) {
        plusBtn = document.createElement('button');
        plusBtn.className = 'lm_plus_button';
        plusBtn.innerText = '+';
        plusBtn.title = 'New connection (Ctrl+N)';
        plusBtn.style.cursor = 'pointer';
        plusBtn.addEventListener('click', (e: MouseEvent) => {
          e.stopPropagation();
          onNewConnection();
        });
        tabsContainer.appendChild(plusBtn);
      }
    });

    // ── Component factories ──────────────────────────────────────────────────

    const register = (
      type: string,
      isTerminal: boolean,
      render: (state: PanelState, container: ComponentContainer) => React.ReactElement,
    ) => {
      gl.registerComponentFactoryFunction(type, (container, itemState) => {
        const state = itemState as PanelState;
        const id = `${type}-${Math.random().toString(36).slice(2)}`;
        const domEl = container.element;

        // GL explicitly sets width/height on this element; we only need
        // overflow containment. Children fill it via position:absolute.
        domEl.style.overflow = 'hidden';

        const el = (
          <div style={{ position: 'absolute', inset: 0, overflow: 'hidden', display: 'flex', flexDirection: 'column' }}>
            {render(state, container)}
          </div>
        );
        addPortal(id, el, domEl, isTerminal);
        container.on('destroy' as never, () => removePortal(id, isTerminal));
      });
    };

    register('terminal', true, (state) => {
      if (state.kind !== 'terminal') return <></>;
      return <TerminalPanel worldName={state.worldName} client={client} />;
    });

    register('spawn', false, (state) => {
      if (state.kind !== 'spawn') return <></>;
      return <SpawnPanel worldName={state.worldName} filter={state.filter} label={state.label} />;
    });

    register('webview', false, (state) => {
      if (state.kind !== 'webview') return <></>;
      return <WebviewPanel url={state.url} />;
    });

    // ── Load saved or default config ──────────────────────────────────────────

    const saved = loadConfig();
    try {
      // saved is ResolvedLayoutConfig (from saveLayout); GL accepts it at runtime
      gl.loadLayout((saved ?? EMPTY_CONFIG) as unknown as LayoutConfig);
    } catch {
      try {
        gl.loadLayout(EMPTY_CONFIG);
      } catch {}
    }

    // Give GL its initial dimensions immediately after load
    gl.setSize(el.offsetWidth, el.offsetHeight);

    // ── Persist on every state change ─────────────────────────────────────────

    gl.on('stateChanged' as never, () => {
      try {
        saveConfig(gl.saveLayout());
      } catch {
        // layout may not be ready yet
      }
    });

    // ── Resize observer — keep GL in sync with container size ─────────────────

    const ro = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (entry) {
        const { width, height } = entry.contentRect;
        gl.setSize(width, height);
      }
    });
    ro.observe(el);

    return () => {
      ro.disconnect();
      gl.destroy();
      glRef.current = null;
      portalsRef.current = [];
      setPortals([]);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className={cn('relative overflow-hidden', className)}>
      {/* Background prompt — visible only when no terminals are open */}
      {terminalCount === 0 && (
        <div className="absolute inset-0 flex flex-col items-center justify-center gap-4 bg-background z-0 select-none">
          <p className="text-muted-foreground text-sm font-mono opacity-50">no worlds connected</p>
          <button
            onClick={onNewConnection}
            className="px-4 py-1.5 rounded border border-border text-xs font-mono text-primary hover:border-primary hover:bg-primary/5 transition-colors cursor-pointer"
          >
            connect world
          </button>
        </div>
      )}

      {/* GoldenLayout container — sits on top of the background */}
      <div
        ref={containerRef}
        className="absolute inset-0"
        style={{ zIndex: terminalCount === 0 ? -1 : 0 }}
      />

      {portals.map(({ id, element, target }) => createPortal(element, target, id))}
    </div>
  );
}
