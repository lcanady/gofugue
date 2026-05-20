import { X } from 'lucide-react';
import { cn } from '@renderer/lib/utils';
import { useMudStore } from '@renderer/store/mud';
import { Button } from '@renderer/components/ui/button';
import type { GofugueClient } from '@gofugue/ipc';

interface WorldTabsProps {
  onConnect: () => void;
  client: GofugueClient;
  className?: string;
}

/**
 * Tab bar showing all open world connections.
 * The active world is highlighted; clicking switches focus.
 * Each tab has an × close button that removes it from the store.
 */
export function WorldTabs({ onConnect, client, className }: WorldTabsProps) {
  const worldOrder = useMudStore((s) => s.worldOrder);
  const worlds = useMudStore((s) => s.worlds);
  const activeWorld = useMudStore((s) => s.activeWorld);
  const setActiveWorld = useMudStore((s) => s.setActiveWorld);
  const removeWorld = useMudStore((s) => s.removeWorld);

  const handleClose = (name: string) => {
    // H3: tell gofugue to disconnect before removing the frontend tab.
    client.cmd(`/dc ${name}`).catch(() => {});
    removeWorld(name);
  };

  return (
    <div
      className={cn(
        'flex items-stretch h-9 border-b border-border bg-card overflow-x-auto scrollbar-thin flex-shrink-0',
        className,
      )}
      role="tablist"
      aria-label="Open worlds"
    >
      {worldOrder.map((name) => {
        const world = worlds[name];
        const isActive = name === activeWorld;
        return (
          <div
            key={name}
            className={cn(
              'flex items-center border-r border-border transition-colors flex-shrink-0',
              isActive
                ? 'bg-background border-b-2 border-b-primary -mb-px'
                : 'text-muted-foreground hover:text-foreground hover:bg-background/50',
            )}
          >
            {/* Tab label — click to activate */}
            <button
              role="tab"
              aria-selected={isActive}
              aria-controls={`panel-${name}`}
              onClick={() => setActiveWorld(name)}
              className={cn(
                'flex items-center gap-1.5 pl-3 pr-1.5 h-full text-sm font-mono whitespace-nowrap',
                'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring',
                'cursor-pointer',
                isActive ? 'text-foreground' : '',
              )}
            >
              {/* Connection indicator dot */}
              <span
                className={cn(
                  'inline-block w-1.5 h-1.5 rounded-full flex-shrink-0',
                  world?.connected ? 'bg-green-500' : 'bg-muted-foreground',
                )}
                aria-label={world?.connected ? 'connected' : 'disconnected'}
              />
              <span className="max-w-[10rem] truncate">{name}</span>
              {world?.lagMS > 0 && (
                <span className="text-[10px] text-muted-foreground">{world.lagMS}ms</span>
              )}
            </button>

            {/* Close button */}
            <button
              onClick={(e) => {
                e.stopPropagation();
                handleClose(name);
              }}
              aria-label={`Close ${name}`}
              className={cn(
                'flex items-center justify-center w-5 h-5 mx-1 rounded',
                'text-muted-foreground hover:text-foreground hover:bg-muted/50',
                'transition-colors cursor-pointer',
                'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring',
              )}
            >
              <X className="w-3 h-3" />
            </button>
          </div>
        );
      })}

      {/* New connection button */}
      <Button
        variant="ghost"
        size="sm"
        className="ml-1 h-full rounded-none text-muted-foreground hover:text-foreground cursor-pointer"
        onClick={onConnect}
        aria-label="Open new connection"
        title="New connection (Ctrl+N)"
      >
        +
      </Button>
    </div>
  );
}
