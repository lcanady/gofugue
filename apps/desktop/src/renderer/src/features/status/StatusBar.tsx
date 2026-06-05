import { useState, useRef } from 'react';
import { Wifi, WifiOff, Clock, Circle, CircleStop, X, Check } from 'lucide-react';
import { cn } from '@renderer/lib/utils';
import { useMudStore } from '@renderer/store/mud';
import type { ConnectionState, GofugueClient } from '@gofugue/ipc';

interface StatusBarProps {
  ipcState: ConnectionState;
  className?: string;
  client?: GofugueClient;
}

/**
 * Thin one-line status bar at the bottom of the window.
 * Shows IPC connection state + per-world connection/lag info.
 * Includes a session logging toggle button.
 */
export function StatusBar({ ipcState, className, client }: StatusBarProps) {
  const activeWorld = useMudStore((s) => s.activeWorld);
  const world = useMudStore((s) => (s.activeWorld ? s.worlds[s.activeWorld] : null));

  const ipcOk = ipcState === 'connected';

  // ── Session Logging State ───────────────────────────────────────────────
  const [isLogging, setIsLogging] = useState(false);
  const [logPath, setLogPath] = useState('');
  const [showLogInput, setShowLogInput] = useState(false);
  const logInputRef = useRef<HTMLInputElement>(null);

  const defaultLogPath = () => {
    const now = new Date();
    const ts = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}_${String(now.getHours()).padStart(2, '0')}${String(now.getMinutes()).padStart(2, '0')}${String(now.getSeconds()).padStart(2, '0')}`;
    return `session_${ts}.log`;
  };

  const handleRecordClick = async () => {
    if (isLogging) {
      // Stop logging
      try {
        await client?.cmd('/log off');
      } catch { /* ignore */ }
      setIsLogging(false);
      setLogPath('');
    } else {
      // Show path input to start
      setLogPath(defaultLogPath());
      setShowLogInput(true);
      setTimeout(() => logInputRef.current?.select(), 50);
    }
  };

  const handleStartLog = async () => {
    const path = logPath.trim() || defaultLogPath();
    try {
      await client?.cmd(`/log ${path}`);
      setIsLogging(true);
      setShowLogInput(false);
    } catch { /* ignore */ }
  };

  const handleCancelLog = () => {
    setShowLogInput(false);
  };

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
        {ipcOk ? 'connected' : ipcState}
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

      {/* Session Logging */}
      {client && (
        <>
          {showLogInput && (
            <div className="flex items-center gap-1 animate-in slide-in-from-right-2 duration-150">
              <input
                ref={logInputRef}
                type="text"
                value={logPath}
                onChange={(e) => setLogPath(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') handleStartLog();
                  if (e.key === 'Escape') handleCancelLog();
                }}
                placeholder="session.log"
                className="h-4 px-1.5 text-[10px] font-mono bg-background border border-border rounded focus:outline-none focus:border-primary w-40"
                id="log-path-input"
                aria-label="Log file path"
              />
              <button
                onClick={handleStartLog}
                className="text-green-500 hover:text-green-400 cursor-pointer focus-visible:outline-none"
                title="Start recording"
                aria-label="Start recording"
              >
                <Check className="w-3 h-3" />
              </button>
              <button
                onClick={handleCancelLog}
                className="text-muted-foreground hover:text-foreground cursor-pointer focus-visible:outline-none"
                title="Cancel"
                aria-label="Cancel recording"
              >
                <X className="w-3 h-3" />
              </button>
            </div>
          )}
          <button
            onClick={handleRecordClick}
            className={cn(
              'flex items-center gap-1 cursor-pointer focus-visible:outline-none transition-colors',
              isLogging
                ? 'text-red-500 hover:text-red-400'
                : 'text-muted-foreground hover:text-foreground',
            )}
            title={isLogging ? 'Stop recording session log' : 'Record session log'}
            aria-label={isLogging ? 'Stop recording' : 'Record session'}
            id="log-record-button"
          >
            {isLogging ? (
              <>
                <Circle className="w-3 h-3 fill-current animate-pulse" />
                <span className="text-[10px]">REC</span>
                <CircleStop className="w-3 h-3" />
              </>
            ) : (
              <Circle className="w-3 h-3" />
            )}
          </button>
          <span className="text-border">│</span>
        </>
      )}

      {/* GoFugue wordmark */}
      <span className="text-muted-foreground/50">GoFugue</span>
    </div>
  );
}
