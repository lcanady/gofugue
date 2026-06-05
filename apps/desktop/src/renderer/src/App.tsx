import { useState, useEffect } from 'react';
import { TooltipProvider } from '@renderer/components/ui/tooltip';
import { WorldManagerDialog } from '@renderer/features/worlds/WorldManagerDialog';
import { GoldenLayoutRoot } from '@renderer/features/layout/GoldenLayoutRoot';
import { InputBar } from '@renderer/features/input/InputBar';
import { StatusBar } from '@renderer/features/status/StatusBar';
import { SearchPanel } from '@renderer/features/terminal/SearchPanel';
import { useGofugue } from '@renderer/hooks/useGofugue';
import { useMudStore } from '@renderer/store/mud';

function isMacOS(): boolean {
  return (
    window.electron?.process?.platform === 'darwin' ||
    navigator.platform.startsWith('Mac') ||
    navigator.userAgent.includes('Macintosh')
  );
}

export function App() {
  const { client, connState } = useGofugue();
  const [worldMgrOpen, setWorldMgrOpen] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const mac = isMacOS();
  const activeWorld = useMudStore((s) => s.activeWorld);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // World Manager: Ctrl+Shift+N / Cmd+Shift+N
      if ((e.ctrlKey || e.metaKey) && e.shiftKey && e.key.toLowerCase() === 'n') {
        e.preventDefault();
        setWorldMgrOpen(true);
        return;
      }
      // Terminal Search: Ctrl+F / Cmd+F
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'f' && !e.shiftKey) {
        e.preventDefault();
        setSearchOpen((prev) => !prev);
        return;
      }
    };
    window.addEventListener('keydown', onKey);
    // Expose toggle for test automation (bypasses Chromium's native Ctrl+F)
    (window as any).__gofugueToggleSearch = () => setSearchOpen((prev) => !prev);
    return () => {
      window.removeEventListener('keydown', onKey);
      delete (window as any).__gofugueToggleSearch;
    };
  }, []);

  return (
    <TooltipProvider delayDuration={600}>
      <div className="flex flex-col h-full bg-background text-foreground">
        {mac && <div className="h-10 flex-shrink-0 titlebar-drag" />}

        <GoldenLayoutRoot
          client={client}
          onNewConnection={() => setWorldMgrOpen(true)}
          className="flex-1 min-h-0"
        />

        {searchOpen && (
          <SearchPanel
            worldName={activeWorld}
            onClose={() => setSearchOpen(false)}
          />
        )}
        {/* Hidden toggle for test automation — keyboard shortcut is the real trigger */}
        <button
          data-testid="search-toggle"
          onClick={() => setSearchOpen((p) => !p)}
          style={{ display: 'none' }}
          aria-hidden="true"
          tabIndex={-1}
        />

        <InputBar client={client} />
        <StatusBar ipcState={connState} client={client} />
      </div>

      <WorldManagerDialog
        open={worldMgrOpen}
        onOpenChange={setWorldMgrOpen}
        client={client}
        connState={connState}
      />
    </TooltipProvider>
  );
}
