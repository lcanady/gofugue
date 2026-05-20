/**
 * WorldManagerDialog — BeipMU-style two-panel world & character manager.
 *
 * Left panel:  tree of World > Character entries with add/expand controls.
 * Right panel: context-sensitive form for the selected world or character.
 * Footer:      Delete · Cancel · Connect (as CharacterName).
 *
 * Passwords are kept in plaintext only while the dialog is open.
 * On blur of the password field (or on Connect), the value is encrypted
 * via Electron safeStorage IPC and stored as base64 in the profile store.
 */
import { useState, useEffect, useCallback, useRef } from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import {
  Globe,
  ChevronRight,
  ChevronDown,
  User,
  Plus,
  Trash2,
  X,
  Lock,
  WifiOff,
  Loader2,
  AlertCircle,
} from 'lucide-react';
import { cn } from '@renderer/lib/utils';
import { Button } from '@renderer/components/ui/button';
import {
  useProfileStore,
  type WorldProfile,
  type CharacterProfile,
} from '@renderer/store/profiles';
import { useMudStore } from '@renderer/store/mud';
import { useLayoutStore } from '@renderer/store/glLayout';
import type { GofugueClient, ConnectionState } from '@gofugue/ipc';

// ── Selection state ────────────────────────────────────────────────────────

type Selection =
  | { kind: 'world'; worldId: string }
  | { kind: 'char'; worldId: string; charId: string }
  | null;

// ── Main dialog ────────────────────────────────────────────────────────────

interface WorldManagerDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  client: GofugueClient;
  connState: ConnectionState;
}

export function WorldManagerDialog({ open, onOpenChange, client, connState }: WorldManagerDialogProps) {
  const {
    worlds,
    addWorld,
    updateWorld,
    deleteWorld,
    addCharacter,
    updateCharacter,
    deleteCharacter,
    setPendingAutoLogin,
    worldNameFromUrl,
  } = useProfileStore();

  const [selection, setSelection] = useState<Selection>(null);
  const [expandedWorlds, setExpandedWorlds] = useState<Set<string>>(new Set());
  const [connectError, setConnectError] = useState<string | null>(null);
  const [connecting, setConnecting] = useState(false);

  // Select first world when dialog opens with nothing selected.
  useEffect(() => {
    if (open && worlds.length > 0 && !selection) {
      setSelection({ kind: 'world', worldId: worlds[0].id });
      setExpandedWorlds(new Set([worlds[0].id]));
    }
  }, [open]); // intentionally only on open change

  const selectedWorld = selection ? worlds.find((w) => w.id === selection.worldId) ?? null : null;
  const selectedChar =
    selection?.kind === 'char' && selectedWorld
      ? selectedWorld.characters.find((c) => c.id === selection.charId) ?? null
      : null;

  const toggleExpand = (worldId: string) =>
    setExpandedWorlds((prev) => {
      const next = new Set(prev);
      next.has(worldId) ? next.delete(worldId) : next.add(worldId);
      return next;
    });

  const handleAddWorld = () => {
    const world = addWorld();
    setExpandedWorlds((prev) => new Set([...prev, world.id]));
    setSelection({ kind: 'world', worldId: world.id });
  };

  const handleAddChar = (worldId: string) => {
    const char = addCharacter(worldId);
    setExpandedWorlds((prev) => new Set([...prev, worldId]));
    setSelection({ kind: 'char', worldId, charId: char.id });
  };

  const handleDelete = () => {
    if (!selection) return;
    if (selection.kind === 'world') {
      deleteWorld(selection.worldId);
      setSelection(worlds.length > 1 ? { kind: 'world', worldId: worlds[0].id } : null);
    } else {
      deleteCharacter(selection.worldId, selection.charId);
      setSelection({ kind: 'world', worldId: selection.worldId });
    }
  };

  const handleConnect = async () => {
    setConnectError(null);
    if (connState !== 'connected') {
      setConnectError('Not connected to gofugue backend. Is "gf --headless" running?');
      return;
    }

    // Read fresh from store to avoid stale-closure URL (user may have just typed it).
    const freshWorld = useProfileStore.getState().worlds.find(
      (w) => w.id === selection?.worldId,
    );
    if (!freshWorld) return;

    // Basic URL validation — must have a non-empty host.
    let host = '';
    try {
      const u = new URL(freshWorld.url.replace(/^mud/, 'tcp').replace(/^muds/, 'tcp'));
      host = u.hostname;
    } catch {
      // ignore parse errors below
    }
    if (!host) {
      setConnectError('Enter a valid URL first, e.g. mud://your.game.org:4000');
      return;
    }

    const freshChar =
      selection?.kind === 'char'
        ? freshWorld.characters.find((c) => c.id === selection.charId)
        : null;

    if (freshChar) {
      const worldName = worldNameFromUrl(freshWorld.url);
      setPendingAutoLogin(worldName, freshChar.id);
    }

    setConnecting(true);
    try {
      await client.cmd(`/connect ${freshWorld.url}`);

      const worldName = worldNameFromUrl(freshWorld.url);
      useMudStore.getState().ensureWorld(worldName);
      // Open a terminal panel immediately — this ensures error messages from
      // refused/failed connections are visible even without a CONNECT hook.
      useLayoutStore.getState().ensureTerminalForWorld(worldName);

      // Hydrate real connection state — handles "already connected" where no
      // new STATUS event fires from gofugue.
      client.worldsStatus().then((statuses) => {
        const store = useMudStore.getState();
        for (const w of statuses) {
          store.ensureWorld(w.name);
          store.updateStatus({ WorldName: w.name, Connected: w.connected, LagMS: 0 });
        }
      }).catch(() => {});

      onOpenChange(false);
    } catch (err) {
      setConnectError(err instanceof Error ? err.message : 'Connection failed');
    } finally {
      setConnecting(false);
    }
  };

  const connectLabel =
    selection?.kind === 'char' && selectedChar
      ? `Connect as ${selectedChar.name}`
      : 'Connect';

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 bg-black/60 backdrop-blur-[2px] z-50" />
        <Dialog.Content
          aria-label="Worlds"
          className={cn(
            'fixed left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2 z-50',
            'w-[720px] h-[500px] bg-card border border-border rounded-xl shadow-2xl',
            'flex flex-col overflow-hidden',
            'focus:outline-none',
          )}
        >
          {/* Header */}
          <div className="flex items-center justify-between px-4 py-3 border-b border-border flex-shrink-0">
            <Dialog.Title className="text-sm font-semibold text-foreground">Worlds</Dialog.Title>
            <Dialog.Close className="text-muted-foreground hover:text-foreground transition-colors cursor-pointer focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring rounded p-0.5">
              <X className="w-4 h-4" />
            </Dialog.Close>
          </div>

          {/* Body */}
          <div className="flex flex-1 min-h-0">
            {/* Left panel — world/character tree */}
            <div className="w-56 border-r border-border flex flex-col bg-background/40">
              <div className="flex-1 overflow-y-auto py-1 min-h-0">
                {worlds.length === 0 && (
                  <p className="text-xs text-muted-foreground px-3 py-4">No worlds yet</p>
                )}
                {worlds.map((world) => (
                  <WorldTreeItem
                    key={world.id}
                    world={world}
                    expanded={expandedWorlds.has(world.id)}
                    selection={selection}
                    onToggle={() => toggleExpand(world.id)}
                    onSelectWorld={() => setSelection({ kind: 'world', worldId: world.id })}
                    onSelectChar={(charId) =>
                      setSelection({ kind: 'char', worldId: world.id, charId })
                    }
                    onAddChar={() => handleAddChar(world.id)}
                  />
                ))}
              </div>
              <div className="p-2 border-t border-border">
                <button
                  onClick={handleAddWorld}
                  className="flex items-center gap-1.5 w-full px-2 py-1.5 text-xs text-muted-foreground hover:text-foreground hover:bg-muted/30 rounded transition-colors cursor-pointer"
                >
                  <Plus className="w-3 h-3" />
                  New World
                </button>
              </div>
            </div>

            {/* Right panel — edit form */}
            <div className="flex-1 min-w-0 overflow-y-auto">
              {!selection ? (
                <div className="flex items-center justify-center h-full text-muted-foreground text-sm">
                  Select a world or character to edit
                </div>
              ) : selection.kind === 'world' && selectedWorld ? (
                <WorldForm
                  key={selectedWorld.id}
                  world={selectedWorld}
                  onSave={(patch) => updateWorld(selectedWorld.id, patch)}
                />
              ) : selection.kind === 'char' && selectedChar && selectedWorld ? (
                <CharacterForm
                  key={selectedChar.id}
                  char={selectedChar}
                  onSave={(patch) => updateCharacter(selectedWorld.id, selectedChar.id, patch)}
                />
              ) : null}
            </div>
          </div>

          {/* Footer */}
          <div className="flex flex-col gap-2 px-4 py-3 border-t border-border flex-shrink-0">
            {/* Error / IPC status strip */}
            {connectError ? (
              <div className="flex items-center gap-1.5 text-xs text-destructive">
                <AlertCircle className="w-3.5 h-3.5 flex-shrink-0" />
                {connectError}
              </div>
            ) : connState !== 'connected' ? (
              <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <WifiOff className="w-3.5 h-3.5 flex-shrink-0" />
                gofugue not running — start it with <code className="font-mono bg-muted/50 px-1 rounded">gf --headless</code>
              </div>
            ) : null}

            <div className="flex items-center justify-between">
              <Button
                variant="destructive"
                size="sm"
                onClick={handleDelete}
                disabled={!selection}
                className="cursor-pointer"
              >
                <Trash2 className="w-3.5 h-3.5 mr-1.5" />
                Delete
              </Button>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => onOpenChange(false)}
                  className="cursor-pointer"
                >
                  Cancel
                </Button>
                <Button
                  size="sm"
                  onClick={handleConnect}
                  disabled={!selectedWorld || connecting}
                  className="cursor-pointer min-w-[90px]"
                >
                  {connecting ? (
                    <Loader2 className="w-3.5 h-3.5 animate-spin" />
                  ) : (
                    connectLabel
                  )}
                </Button>
              </div>
            </div>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

// ── Tree item ──────────────────────────────────────────────────────────────

interface WorldTreeItemProps {
  world: WorldProfile;
  expanded: boolean;
  selection: Selection;
  onToggle: () => void;
  onSelectWorld: () => void;
  onSelectChar: (charId: string) => void;
  onAddChar: () => void;
}

function WorldTreeItem({
  world,
  expanded,
  selection,
  onToggle,
  onSelectWorld,
  onSelectChar,
  onAddChar,
}: WorldTreeItemProps) {
  const isWorldSelected = selection?.kind === 'world' && selection.worldId === world.id;

  return (
    <div>
      {/* World row */}
      <div className="flex items-center group pr-1">
        <button
          onClick={onToggle}
          className="flex-shrink-0 p-1 text-muted-foreground hover:text-foreground transition-colors cursor-pointer focus-visible:outline-none"
          tabIndex={-1}
          aria-label={expanded ? 'Collapse' : 'Expand'}
        >
          {expanded ? (
            <ChevronDown className="w-3 h-3" />
          ) : (
            <ChevronRight className="w-3 h-3" />
          )}
        </button>
        <button
          onClick={onSelectWorld}
          className={cn(
            'flex-1 flex items-center gap-1.5 px-1 py-1 text-xs rounded min-w-0 transition-colors cursor-pointer focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring',
            isWorldSelected
              ? 'bg-primary/15 text-primary'
              : 'text-foreground hover:bg-muted/30',
          )}
        >
          <Globe className="w-3 h-3 flex-shrink-0 opacity-60" />
          <span className="truncate font-medium">{world.name}</span>
        </button>
        {/* Add character — appears on hover */}
        <button
          onClick={onAddChar}
          className="opacity-0 group-hover:opacity-100 flex-shrink-0 p-1 text-muted-foreground hover:text-foreground transition-all cursor-pointer focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring rounded"
          title="Add character"
          aria-label="Add character"
        >
          <Plus className="w-3 h-3" />
        </button>
      </div>

      {/* Character rows */}
      {expanded &&
        world.characters.map((char) => {
          const isCharSelected =
            selection?.kind === 'char' &&
            selection.worldId === world.id &&
            selection.charId === char.id;
          return (
            <button
              key={char.id}
              onClick={() => onSelectChar(char.id)}
              className={cn(
                'flex items-center gap-1.5 w-full pl-8 pr-3 py-1 text-xs rounded transition-colors cursor-pointer focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring',
                isCharSelected
                  ? 'bg-primary/15 text-primary'
                  : 'text-muted-foreground hover:text-foreground hover:bg-muted/30',
              )}
            >
              <User className="w-3 h-3 flex-shrink-0 opacity-60" />
              <span className="truncate">{char.name}</span>
              {char.encryptedPassword && (
                <Lock className="w-2.5 h-2.5 flex-shrink-0 opacity-40 ml-auto" />
              )}
            </button>
          );
        })}
    </div>
  );
}

// ── World form ─────────────────────────────────────────────────────────────

function WorldForm({
  world,
  onSave,
}: {
  world: WorldProfile;
  onSave: (patch: Partial<WorldProfile>) => void;
}) {
  const [name, setName] = useState(world.name);
  const [url, setUrl] = useState(world.url);
  const [encoding, setEncoding] = useState(world.encoding);
  const [gmcp, setGmcp] = useState(world.gmcp);
  const [notes, setNotes] = useState(world.notes);

  // Sync back to store on every field change.
  const save = useCallback(
    (patch: Partial<WorldProfile>) => onSave(patch),
    [onSave],
  );

  return (
    <div className="flex flex-col gap-5 p-5">
      <SectionLabel>World</SectionLabel>

      <FieldGroup>
        <Label>Name</Label>
        <Input
          value={name}
          onChange={(v) => { setName(v); save({ name: v }); }}
          placeholder="My MUD"
        />
      </FieldGroup>

      <FieldGroup>
        <Label>URL</Label>
        <Input
          value={url}
          onChange={(v) => { setUrl(v); save({ url: v }); }}
          placeholder="mud://host:4000"
          monospace
        />
        <FieldHint>mud:// · muds:// · ws:// · wss://</FieldHint>
      </FieldGroup>

      <FieldGroup>
        <Label>Encoding</Label>
        <select
          value={encoding}
          onChange={(e) => { setEncoding(e.target.value); save({ encoding: e.target.value }); }}
          className="w-full bg-background border border-border rounded-md px-3 py-1.5 text-sm text-foreground focus:outline-none focus:ring-1 focus:ring-ring cursor-pointer"
        >
          <option value="utf-8">UTF-8</option>
          <option value="latin1">Latin-1 (ISO-8859-1)</option>
        </select>
      </FieldGroup>

      <FieldGroup>
        <label className="flex items-center gap-2 cursor-pointer select-none">
          <input
            type="checkbox"
            checked={gmcp}
            onChange={(e) => { setGmcp(e.target.checked); save({ gmcp: e.target.checked }); }}
            className="accent-primary cursor-pointer"
          />
          <span className="text-sm text-foreground">Enable GMCP</span>
        </label>
      </FieldGroup>

      <FieldGroup>
        <Label>Notes</Label>
        <Textarea
          value={notes}
          onChange={(v) => { setNotes(v); save({ notes: v }); }}
          rows={3}
          placeholder="Optional notes about this world…"
        />
      </FieldGroup>
    </div>
  );
}

// ── Character form ─────────────────────────────────────────────────────────

function CharacterForm({
  char,
  onSave,
}: {
  char: CharacterProfile;
  onSave: (patch: Partial<CharacterProfile>) => void;
}) {
  const [name, setName] = useState(char.name);
  const [connectString, setConnectString] = useState(char.connectString);
  const [password, setPassword] = useState('');
  const [notes, setNotes] = useState(char.notes);
  const passwordDirty = useRef(false);

  // Decrypt stored password on character load.
  useEffect(() => {
    passwordDirty.current = false;
    if (char.encryptedPassword && window.gofugue?.decryptPassword) {
      window.gofugue.decryptPassword(char.encryptedPassword).then((p) => {
        setPassword(p);
      });
    } else {
      setPassword('');
    }
  }, [char.id]);

  const encryptAndSave = useCallback(async () => {
    if (!passwordDirty.current) return;
    passwordDirty.current = false;
    if (!password) {
      onSave({ encryptedPassword: '' });
      return;
    }
    if (window.gofugue?.encryptPassword) {
      const enc = await window.gofugue.encryptPassword(password);
      onSave({ encryptedPassword: enc });
    }
  }, [password, onSave]);

  return (
    <div className="flex flex-col gap-5 p-5">
      <SectionLabel>Character</SectionLabel>

      <FieldGroup>
        <Label>Name</Label>
        <Input
          value={name}
          onChange={(v) => { setName(v); onSave({ name: v }); }}
          placeholder="PlayerName"
        />
      </FieldGroup>

      <FieldGroup>
        <Label>Connect string</Label>
        <Textarea
          value={connectString}
          onChange={(v) => { setConnectString(v); onSave({ connectString: v }); }}
          rows={4}
          placeholder={'connect {name} {password}\nother setup command'}
          monospace
        />
        <FieldHint>One command per line. Use &#123;password&#125; as a macro.</FieldHint>
      </FieldGroup>

      <FieldGroup>
        <Label>Password</Label>
        <div className="relative">
          <Input
            type="password"
            value={password}
            onChange={(v) => {
              setPassword(v);
              passwordDirty.current = true;
            }}
            onBlur={encryptAndSave}
            placeholder="Stored encrypted via OS keychain"
          />
          <Lock className="absolute right-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground pointer-events-none" />
        </div>
        <FieldHint>Encrypted with OS safeStorage (same as browser password managers).</FieldHint>
      </FieldGroup>

      <FieldGroup>
        <Label>Notes</Label>
        <Textarea
          value={notes}
          onChange={(v) => { setNotes(v); onSave({ notes: v }); }}
          rows={2}
          placeholder="Optional notes…"
        />
      </FieldGroup>
    </div>
  );
}

// ── Small form primitives ──────────────────────────────────────────────────

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="text-[10px] font-semibold uppercase tracking-widest text-muted-foreground">
      {children}
    </div>
  );
}

function FieldGroup({ children }: { children: React.ReactNode }) {
  return <div className="flex flex-col gap-1.5">{children}</div>;
}

function Label({ children }: { children: React.ReactNode }) {
  return <div className="text-xs font-medium text-muted-foreground">{children}</div>;
}

function FieldHint({ children }: { children: React.ReactNode }) {
  return <div className="text-[11px] text-muted-foreground/60">{children}</div>;
}

function Input({
  value,
  onChange,
  onBlur,
  placeholder,
  type = 'text',
  monospace = false,
}: {
  value: string;
  onChange: (v: string) => void;
  onBlur?: () => void;
  placeholder?: string;
  type?: string;
  monospace?: boolean;
}) {
  return (
    <input
      type={type}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      onBlur={onBlur}
      placeholder={placeholder}
      className={cn(
        'w-full bg-background border border-border rounded-md px-3 py-1.5 text-sm text-foreground',
        'placeholder:text-muted-foreground/40',
        'focus:outline-none focus:ring-1 focus:ring-ring',
        monospace && 'font-mono',
      )}
    />
  );
}

function Textarea({
  value,
  onChange,
  rows = 3,
  placeholder,
  monospace = false,
}: {
  value: string;
  onChange: (v: string) => void;
  rows?: number;
  placeholder?: string;
  monospace?: boolean;
}) {
  return (
    <textarea
      value={value}
      onChange={(e) => onChange(e.target.value)}
      rows={rows}
      placeholder={placeholder}
      className={cn(
        'w-full bg-background border border-border rounded-md px-3 py-2 text-sm text-foreground resize-none',
        'placeholder:text-muted-foreground/40',
        'focus:outline-none focus:ring-1 focus:ring-ring',
        monospace && 'font-mono',
      )}
    />
  );
}
