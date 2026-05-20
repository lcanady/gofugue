/**
 * Returns the world name for the currently focused terminal panel in GoldenLayout,
 * so InputBar knows where to send text.
 *
 * GoldenLayout fires a 'focus' event on ComponentItem when a tab is clicked.
 * We listen to that and store the active world name.
 */
import { useState, useEffect } from 'react';
import { useLayoutStore } from '@renderer/store/glLayout';
import type { ComponentItem } from 'golden-layout';
import type { PanelState } from '@renderer/features/layout/GoldenLayoutRoot';

export function useActiveWorld(): string | null {
  const gl = useLayoutStore((s) => s.gl);
  const [activeWorld, setActiveWorld] = useState<string | null>(null);

  useEffect(() => {
    if (!gl) return;

    const onFocus = (component: ComponentItem) => {
      const state = component.toConfig().componentState as PanelState | undefined;
      if (state?.kind === 'terminal') {
        setActiveWorld(state.worldName);
      } else {
        // Non-terminal panel focused — keep last world active for input
      }
    };

    gl.on('focus' as never, onFocus);
    return () => gl.off('focus' as never, onFocus);
  }, [gl]);

  return activeWorld;
}
