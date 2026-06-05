import { useState, useRef, useEffect, useCallback } from 'react';
import { Search, X, ChevronUp, ChevronDown } from 'lucide-react';
import { cn } from '@renderer/lib/utils';
import { useMudStore } from '@renderer/store/mud';

interface SearchPanelProps {
  /** The world whose scrollback buffer is being searched. */
  worldName: string | null;
  onClose: () => void;
}

/**
 * Inline search panel for terminal scrollback.
 * Rendered above the terminal area; toggled by Ctrl+F / Cmd+F in App.tsx.
 *
 * Highlights matching lines in the virtual list by injecting a data attribute
 * that CSS can target, and scrolls to each hit via the virtualizer.
 */
export function SearchPanel({ worldName, onClose }: SearchPanelProps) {
  const [query, setQuery] = useState('');
  const [matchIdx, setMatchIdx] = useState(-1);
  const inputRef = useRef<HTMLInputElement>(null);

  const lines = useMudStore((s) =>
    worldName ? (s.worlds[worldName]?.lines ?? []) : [],
  );

  // Focus input when panel mounts
  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  // Reset match when query or world changes
  useEffect(() => {
    setMatchIdx(-1);
  }, [query, worldName]);

  const matchingIndices = useCallback(() => {
    if (!query.trim()) return [];
    const q = query.toLowerCase();
    return lines
      .map((l, i) => ({ i, text: l.Text ?? '' }))
      .filter(({ text }) => text.toLowerCase().includes(q))
      .map(({ i }) => i);
  }, [query, lines]);

  const scrollToMatch = useCallback(
    (idx: number) => {
      if (!worldName) return;
      const matches = matchingIndices();
      if (matches.length === 0) return;
      const lineIdx = matches[idx];
      // Dispatch a custom event that TerminalPane listens for
      window.dispatchEvent(
        new CustomEvent('gofugue:search-scroll', {
          detail: { worldName, lineIndex: lineIdx, query },
        }),
      );
    },
    [worldName, matchingIndices, query],
  );

  const handleNext = useCallback(() => {
    const matches = matchingIndices();
    if (matches.length === 0) return;
    const next = (matchIdx + 1) % matches.length;
    setMatchIdx(next);
    scrollToMatch(next);
  }, [matchIdx, matchingIndices, scrollToMatch]);

  const handlePrev = useCallback(() => {
    const matches = matchingIndices();
    if (matches.length === 0) return;
    const prev = (matchIdx - 1 + matches.length) % matches.length;
    setMatchIdx(prev);
    scrollToMatch(prev);
  }, [matchIdx, matchingIndices, scrollToMatch]);

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      if (e.shiftKey) handlePrev();
      else handleNext();
    } else if (e.key === 'Escape') {
      onClose();
    }
    e.stopPropagation();
  };

  const matches = matchingIndices();
  const hasQuery = query.trim().length > 0;
  const matchCount = matches.length;

  return (
    <div
      className={cn(
        'flex items-center gap-2 px-3 py-1.5 border-b border-border bg-card/80 backdrop-blur-sm',
        'animate-in slide-in-from-top-1 duration-150 flex-shrink-0',
      )}
      role="search"
      aria-label="Terminal search"
    >
      <Search className="w-3.5 h-3.5 text-muted-foreground flex-shrink-0" />

      <input
        ref={inputRef}
        id="terminal-search-input"
        type="text"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        onKeyDown={handleKeyDown}
        placeholder="Search scrollback…"
        className={cn(
          'flex-1 bg-transparent text-sm text-foreground placeholder:text-muted-foreground/50',
          'focus:outline-none font-mono min-w-0',
          hasQuery && matchCount === 0 && 'text-destructive',
        )}
        aria-label="Search query"
        autoComplete="off"
        spellCheck={false}
      />

      {/* Match counter */}
      {hasQuery && (
        <span className="text-[11px] text-muted-foreground flex-shrink-0 tabular-nums">
          {matchCount === 0
            ? 'No results'
            : `${matchIdx >= 0 ? matchIdx + 1 : 1} / ${matchCount}`}
        </span>
      )}

      {/* Prev / Next navigation */}
      <div className="flex items-center gap-0.5 flex-shrink-0">
        <button
          onClick={handlePrev}
          disabled={matchCount === 0}
          className="p-1 rounded hover:bg-muted/50 text-muted-foreground hover:text-foreground disabled:opacity-30 cursor-pointer disabled:cursor-default transition-colors focus-visible:outline-none"
          title="Previous match (Shift+Enter)"
          aria-label="Previous match"
        >
          <ChevronUp className="w-3.5 h-3.5" />
        </button>
        <button
          onClick={handleNext}
          disabled={matchCount === 0}
          className="p-1 rounded hover:bg-muted/50 text-muted-foreground hover:text-foreground disabled:opacity-30 cursor-pointer disabled:cursor-default transition-colors focus-visible:outline-none"
          title="Next match (Enter)"
          aria-label="Next match"
        >
          <ChevronDown className="w-3.5 h-3.5" />
        </button>
      </div>

      {/* Close */}
      <button
        onClick={onClose}
        className="p-1 rounded hover:bg-muted/50 text-muted-foreground hover:text-foreground cursor-pointer transition-colors focus-visible:outline-none"
        title="Close search (Escape)"
        aria-label="Close search"
      >
        <X className="w-3.5 h-3.5" />
      </button>
    </div>
  );
}
