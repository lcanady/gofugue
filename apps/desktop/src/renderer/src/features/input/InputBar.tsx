import {
  useState,
  useRef,
  useCallback,
  useEffect,
  type KeyboardEvent,
  type FormEvent,
} from 'react';
import { Send } from 'lucide-react';
import { cn } from '@renderer/lib/utils';
import { useMudStore } from '@renderer/store/mud';
import { useProfileStore } from '@renderer/store/profiles';
import { useActiveWorld } from '@renderer/hooks/useActiveWorld';
import type { GofugueClient } from '@gofugue/ipc';

const MAX_HISTORY = 200;
/** Max height before the textarea scrolls instead of growing (~5 lines). */
const MAX_INPUT_HEIGHT = 120;

interface InputBarProps {
  client: GofugueClient;
  className?: string;
}

/**
 * Command input bar with local history (↑/↓), speedwalk passthrough,
 * and /command detection.
 *
 * The textarea auto-expands as text wraps, up to MAX_INPUT_HEIGHT px,
 * then scrolls internally.
 */
export function InputBar({ client, className }: InputBarProps) {
  const [text, setText] = useState('');
  const [historyIdx, setHistoryIdx] = useState(-1);
  const history = useRef<string[]>([]);
  const savedDraft = useRef(''); // saves in-progress text while browsing history
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Derived from the focused pane's active terminal panel.
  const activeWorld = useActiveWorld();
  // Still subscribe to mud store so the reconnect check has world state.
  const worlds = useMudStore((s) => s.worlds);

  /** Shrink to fit content, capped at MAX_INPUT_HEIGHT. */
  const resize = useCallback(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, MAX_INPUT_HEIGHT)}px`;
  }, []);

  // Resize whenever text changes (including programmatic updates from history nav).
  useEffect(() => {
    resize();
  }, [text, resize]);

  const submit = useCallback(
    async (value: string) => {
      const trimmed = value.trim();

      // Empty Enter on a disconnected world → reconnect.
      if (!trimmed) {
        if (!activeWorld) return;
        const worldState = worlds[activeWorld];
        if (worldState && !worldState.connected) {
          const profileStore = useProfileStore.getState();
          const profile = profileStore.worlds.find(
            (p) => profileStore.worldNameFromUrl(p.url) === activeWorld,
          );
          if (profile) {
            await client.cmd(`/connect ${profile.url}`);
          }
        }
        return;
      }

      // Add to local history (deduplicate at head).
      if (history.current[0] !== trimmed) {
        history.current = [trimmed, ...history.current].slice(0, MAX_HISTORY);
      }
      setHistoryIdx(-1);
      savedDraft.current = '';
      setText('');
      if (textareaRef.current) textareaRef.current.style.height = 'auto';

      // Route to gofugue IPC.
      if (trimmed.startsWith('/')) {
        await client.cmd(trimmed, activeWorld ?? undefined);
      } else {
        await client.input(trimmed, activeWorld ?? undefined);
      }
    },
    [client, activeWorld],
  );

  const handleKeyDown = useCallback(
    (e: KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'ArrowUp' && !e.shiftKey) {
        e.preventDefault();
        const nextIdx = historyIdx + 1;
        if (nextIdx >= history.current.length) return;
        if (historyIdx === -1) savedDraft.current = text;
        setHistoryIdx(nextIdx);
        setText(history.current[nextIdx]);
      } else if (e.key === 'ArrowDown' && !e.shiftKey) {
        e.preventDefault();
        if (historyIdx === -1) return;
        const nextIdx = historyIdx - 1;
        setHistoryIdx(nextIdx);
        setText(nextIdx === -1 ? savedDraft.current : history.current[nextIdx]);
      } else if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        submit(text);
      }
    },
    [historyIdx, text, submit],
  );

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    submit(text);
  };

  return (
    <form
      onSubmit={handleSubmit}
      className={cn(
        'flex items-end gap-2 px-3 py-1.5 min-h-9 border-t border-border bg-card flex-shrink-0 select-none',
        className,
      )}
    >
      {/* World indicator — pinned to bottom when textarea is tall */}
      <span className="text-xs font-mono text-muted-foreground select-none flex-shrink-0 leading-[1.375rem]">
        {activeWorld ?? '—'}
        {' >'}
      </span>

      <textarea
        ref={textareaRef}
        rows={1}
        value={text}
        onChange={(e) => {
          setText(e.target.value);
          setHistoryIdx(-1);
        }}
        onKeyDown={handleKeyDown}
        placeholder="Type a command…"
        autoFocus
        autoComplete="off"
        autoCorrect="off"
        autoCapitalize="off"
        spellCheck={false}
        className={cn(
          'flex-1 bg-transparent font-mono text-sm text-foreground',
          'placeholder:text-muted-foreground/50',
          'focus:outline-none resize-none overflow-y-auto leading-[1.375rem]',
          '[caret-color:hsl(var(--terminal-cursor))]',
        )}
        aria-label="Command input"
      />

      <button
        type="submit"
        disabled={!text.trim()}
        className={cn(
          'flex-shrink-0 text-muted-foreground hover:text-foreground transition-colors',
          'disabled:opacity-30 disabled:cursor-not-allowed cursor-pointer',
          'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring rounded',
          'mb-px', // optical alignment with single-line text
        )}
        aria-label="Send command"
        title="Send (Enter)"
      >
        <Send className="w-4 h-4" />
      </button>
    </form>
  );
}
