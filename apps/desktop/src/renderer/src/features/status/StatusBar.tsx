import { Wifi, WifiOff, Clock } from 'lucide-react';
import { cn } from '@renderer/lib/utils';
import { useMudStore } from '@renderer/store/mud';
import type { ConnectionState } from '@gofugue/ipc';

interface StatusBarProps {
  ipcState: ConnectionState;
  className?: string;
}

/**
 * Thin one-line status bar at the bottom of the window.
 * Shows IPC connection state + per-world connection/lag info.
 */
export function StatusBar({ ipcState, className }: StatusBarProps) {
  const activeWorld = useMudStore((s) => s.activeWorld);
  const world = useMudStore((s) => (s.activeWorld ? s.worlds[s.activeWorld] : null));

  const ipcOk = ipcState === 'connected';

  return (
    <div
      className={cn(
        'flex items-center gap-3 px-3 h-6 text-[11px] font-mono select-none',
        'bg-card border-t border-border text-muted-foreground flex-shrink-0',
        className,
      )}
      role="status"
      aria-live="polite"
    >
      {/* IPC link to gofugue */}
      <span
        className={cn(
          'flex items-center gap-1',
          ipcOk ? 'text-green-500' : 'text-destructive',
        )}
        title={`IPC: ${ipcState}`}
      >
        {ipcOk ? <Wifi className="w-3 h-3" /> : <WifiOff className="w-3 h-3" />}
        {ipcOk ? 'gofugue' : ipcState}
      </span>

      <span className="text-border">│</span>

      {/* Active world */}
      {activeWorld ? (
        <>
          <span className="text-foreground/70 truncate max-w-[12rem]">{activeWorld}</span>
          <span className="text-border">│</span>
          <span
            className={cn(
              world?.connected ? 'text-green-500' : 'text-destructive',
            )}
          >
            {world?.connected ? 'CONNECTED' : 'DISCONNECTED'}
          </span>
          {world?.lagMS ? (
            <>
              <span className="text-border">│</span>
              <span className="flex items-center gap-1">
                <Clock className="w-3 h-3" />
                {world.lagMS}ms
              </span>
            </>
          ) : null}
        </>
      ) : (
        <span className="italic">no world</span>
      )}

      {/* Spacer */}
      <span className="flex-1" />

      {/* GoFugue wordmark */}
      <span className="text-muted-foreground/50">GoFugue</span>
    </div>
  );
}
