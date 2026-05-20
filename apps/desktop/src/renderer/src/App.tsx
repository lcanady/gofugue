import { useState, useEffect } from 'react';
import { TooltipProvider } from '@renderer/components/ui/tooltip';
import { WorldManagerDialog } from '@renderer/features/worlds/WorldManagerDialog';
import { GoldenLayoutRoot } from '@renderer/features/layout/GoldenLayoutRoot';
import { InputBar } from '@renderer/features/input/InputBar';
import { StatusBar } from '@renderer/features/status/StatusBar';
import { useGofugue } from '@renderer/hooks/useGofugue';

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
  const mac = isMacOS();

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === 'n') {
        e.preventDefault();
        setWorldMgrOpen(true);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
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

        <InputBar client={client} />
        <StatusBar ipcState={connState} />
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
