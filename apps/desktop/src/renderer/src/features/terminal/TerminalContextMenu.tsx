/**
 * TerminalContextMenu — right-click menu for the terminal output area.
 *
 * "Talking" items pre-fill the MUD communication commands with the selected
 * text, matching BeipMU's context-menu pattern.
 */
import { useState } from 'react';
import * as ContextMenu from '@radix-ui/react-context-menu';
import { cn } from '@renderer/lib/utils';
import { useMudStore } from '@renderer/store/mud';
import { TriggerEditorDialog } from '@renderer/features/triggers/TriggerEditorDialog';
import type { GofugueClient } from '@gofugue/ipc';

interface TerminalContextMenuProps {
  children: React.ReactNode;
  client: GofugueClient;
}

function getSelection(): string {
  return window.getSelection()?.toString().trim() ?? '';
}

export function TerminalContextMenu({ children, client }: TerminalContextMenuProps) {
  const activeWorld = useMudStore((s) => s.activeWorld);
  const clearWorld = useMudStore((s) => s.clearWorld);

  const [triggerEditorOpen, setTriggerEditorOpen] = useState(false);
  const [triggerPattern, setTriggerPattern] = useState('');

  const send = (text: string) => {
    if (!activeWorld) return;
    client.input(text, activeWorld);
  };

  const sendWithSelection = (prefix: string) => {
    const sel = getSelection();
    if (sel) send(`${prefix} ${sel}`);
  };

  const copySelection = async () => {
    const sel = getSelection();
    if (sel) await navigator.clipboard.writeText(sel);
  };

  const handleClearOutput = () => {
    if (activeWorld) clearWorld(activeWorld);
  };

  return (
    <ContextMenu.Root>
      {/*
       * DO NOT use asChild here: TerminalPane does not forward refs or spread
       * unknown props, so asChild would drop the onContextMenu handler.
       * A block wrapper div fills the parent and owns the right-click surface.
       */}
      <ContextMenu.Trigger asChild>
        <div className="h-full w-full">{children}</div>
      </ContextMenu.Trigger>

      <ContextMenu.Portal>
        <ContextMenu.Content
          className={cn(
            'z-50 min-w-[180px] overflow-hidden rounded-lg border border-border bg-card shadow-xl',
            'p-1 text-sm text-foreground',
            'animate-in fade-in-0 zoom-in-95',
          )}
        >
          {/* Clipboard */}
          <MenuItem onSelect={copySelection}>Copy</MenuItem>

          <ContextMenu.Separator className="my-1 h-px bg-border mx-1" />

          {/* Talking items — pre-fill MUD communication commands */}
          <MenuLabel>Talk</MenuLabel>
          <MenuItem onSelect={() => sendWithSelection('say')}>Say selection</MenuItem>
          <MenuItem onSelect={() => sendWithSelection('emote')}>Emote selection</MenuItem>
          <MenuItem onSelect={() => sendWithSelection('tell')}>Tell… (add target)</MenuItem>

          <ContextMenu.Separator className="my-1 h-px bg-border mx-1" />

          {/* Trigger creation — placeholder for future trigger engine UI */}
          <MenuItem
            onSelect={() => {
              const sel = getSelection();
              if (sel) {
                setTriggerPattern(sel);
                setTriggerEditorOpen(true);
              }
            }}
            disabled={false}
          >
            Create trigger from selection
          </MenuItem>

          <ContextMenu.Separator className="my-1 h-px bg-border mx-1" />

          {/* Destructive */}
          <MenuItem onSelect={handleClearOutput} className="text-destructive focus:text-destructive">
            Clear output
          </MenuItem>
        </ContextMenu.Content>
      </ContextMenu.Portal>

      <TriggerEditorDialog
        open={triggerEditorOpen}
        onOpenChange={setTriggerEditorOpen}
        initialPattern={triggerPattern}
      />
    </ContextMenu.Root>
  );
}

// ── Small menu primitives ──────────────────────────────────────────────────

function MenuItem({
  children,
  onSelect,
  disabled,
  className,
}: {
  children: React.ReactNode;
  onSelect?: () => void;
  disabled?: boolean;
  className?: string;
}) {
  return (
    <ContextMenu.Item
      onSelect={onSelect}
      disabled={disabled}
      className={cn(
        'flex items-center px-2 py-1.5 rounded-md cursor-pointer',
        'select-none outline-none transition-colors',
        'focus:bg-muted/60',
        disabled && 'opacity-40 cursor-default pointer-events-none',
        className,
      )}
    >
      {children}
    </ContextMenu.Item>
  );
}

function MenuLabel({ children }: { children: React.ReactNode }) {
  return (
    <ContextMenu.Label className="px-2 py-1 text-[10px] font-semibold uppercase tracking-widest text-muted-foreground">
      {children}
    </ContextMenu.Label>
  );
}
