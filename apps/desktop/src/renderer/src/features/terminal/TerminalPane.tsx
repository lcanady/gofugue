import { useEffect, useRef, useCallback } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { ANSILine } from '@gofugue/ansi-renderer';
import { useMudStore } from '@renderer/store/mud';
import { cn } from '@renderer/lib/utils';
import type { WorldLineEvent } from '@gofugue/ipc';

/** Stable empty array — avoids creating a new reference on every selector call. */
const EMPTY_LINES: WorldLineEvent[] = [];

interface TerminalPaneProps {
  worldName: string;
  className?: string;
}

/**
 * Virtualized terminal output pane for a single world.
 *
 * - Renders up to 50k lines without DOM overhead.
 * - Auto-scrolls to bottom unless the user has scrolled up.
 * - Re-engages live-tail on scrolling back to bottom.
 */
export function TerminalPane({ worldName, className }: TerminalPaneProps) {
  const lines = useMudStore((s) => s.worlds[worldName]?.lines ?? EMPTY_LINES);
  const connected = useMudStore((s) => s.worlds[worldName]?.connected ?? false);
  const statusReceived = useMudStore((s) => s.worlds[worldName]?.statusReceived ?? false);
  const scrollLocked = useMudStore((s) => s.worlds[worldName]?.scrollLocked ?? false);
  const setScrollLocked = useMudStore((s) => s.setScrollLocked);

  const parentRef = useRef<HTMLDivElement>(null);
  const atBottomRef = useRef(true);

  const virtualizer = useVirtualizer({
    count: lines.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 19.6, // ~1.4em × 14px (font-size: 14px in terminal-pane)
    overscan: 40,
  });

  // Auto-scroll to bottom when new lines arrive and user isn't scrolled up.
  useEffect(() => {
    if (!scrollLocked && lines.length > 0) {
      virtualizer.scrollToIndex(lines.length - 1, { align: 'end' });
    }
  }, [lines.length, scrollLocked, virtualizer]);

  const handleScroll = useCallback(() => {
    const el = parentRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    const isAtBottom = distanceFromBottom < 32; // 32px threshold
    atBottomRef.current = isAtBottom;

    if (isAtBottom && scrollLocked) {
      setScrollLocked(worldName, false);
    } else if (!isAtBottom && !scrollLocked) {
      setScrollLocked(worldName, true);
    }
  }, [worldName, scrollLocked, setScrollLocked]);

  const items = virtualizer.getVirtualItems();

  if (lines.length === 0) {
    const msg = statusReceived && !connected
      ? `Disconnected from ${worldName}`
      : statusReceived && connected
        ? `Connected to ${worldName} — type to play`
        : `Connecting to ${worldName}…`;
    return (
      <div
        className={cn(
          'terminal-pane overflow-hidden flex items-center justify-center',
          className,
        )}
        aria-live="polite"
        aria-label={`Terminal output for ${worldName}`}
      >
        <p
          className={cn(
            'text-xs font-mono',
            statusReceived && !connected
              ? 'text-destructive/70'
              : statusReceived && connected
                ? 'text-green-500/70'
                : 'text-muted-foreground animate-pulse',
          )}
        >
          {msg}
        </p>
      </div>
    );
  }

  return (
    <div
      ref={parentRef}
      onScroll={handleScroll}
      className={cn(
        'terminal-pane overflow-y-auto overflow-x-hidden scrollbar-thin relative',
        className,
      )}
      aria-live="polite"
      aria-relevant="additions"
      aria-label={`Terminal output for ${worldName}`}
    >
      {/* Scroll-lock indicator */}
      {scrollLocked && (
        <div
          className="sticky top-0 z-10 flex items-center justify-center py-0.5 text-xs font-mono text-muted-foreground bg-muted/80 backdrop-blur-sm cursor-pointer"
          onClick={() => {
            setScrollLocked(worldName, false);
            virtualizer.scrollToIndex(lines.length - 1, { align: 'end' });
          }}
          role="button"
          tabIndex={0}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              setScrollLocked(worldName, false);
              virtualizer.scrollToIndex(lines.length - 1, { align: 'end' });
            }
          }}
        >
          ↑ Scrolled — click to jump to bottom
        </div>
      )}

      {/* TanStack Virtual outer spacer */}
      <div
        style={{
          height: `${virtualizer.getTotalSize()}px`,
          width: '100%',
          position: 'relative',
        }}
      >
        {/* Rendered items positioned absolutely within the outer spacer */}
        <div
          style={{
            position: 'absolute',
            top: 0,
            left: 0,
            width: '100%',
            transform: `translateY(${items[0]?.start ?? 0}px)`,
          }}
        >
          {items.map((vItem) => {
            const line = lines[vItem.index];
            if (!line) return null;
            const isLocal = line.WorldName === 'local';
            return (
              <ANSILine
                key={vItem.key}
                event={line}
                className={cn('terminal-line', isLocal && 'text-red-400')}
                data-index={vItem.index}
                ref={virtualizer.measureElement}
              />
            );
          })}
        </div>
      </div>
    </div>
  );
}
