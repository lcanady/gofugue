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
  type TriggerProfile,
  type AliasProfile,
  type TimerProfile,
  type KeyBindingProfile,
  type VariableProfile,
} from '@renderer/store/profiles';
import { useMudStore } from '@renderer/store/mud';
import { useLayoutStore } from '@renderer/store/glLayout';
import type { GofugueClient, ConnectionState } from '@gofugue/ipc';
import { syncWorldMacros, rescheduleTimers } from '@renderer/hooks/useGofugue';

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

  const handleConnect = async (targetWorldIdOrEvent?: any, targetCharId?: string) => {
    const targetWorldId = typeof targetWorldIdOrEvent === 'string' ? targetWorldIdOrEvent : undefined;
    const wId = targetWorldId ?? selection?.worldId;
    const cId = targetCharId ?? (selection?.kind === 'char' ? selection.charId : undefined);

    setConnectError(null);
    if (connState !== 'connected') {
      setConnectError('Connection service is not running. Please restart the application.');
      return;
    }

    // Read fresh from store to avoid stale-closure URL (user may have just typed it).
    const freshWorld = useProfileStore.getState().worlds.find(
      (w) => w.id === wId,
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
      cId
        ? freshWorld.characters.find((c) => c.id === cId)
        : null;

    const worldName = worldNameFromUrl(freshWorld.url);
    if (freshChar) {
      setPendingAutoLogin(worldName, freshChar.id);
    }

    setConnecting(true);
    try {
      useMudStore.getState().setConnectingWorld(worldName, true);
      await client.cmd(`/connect ${freshWorld.url}`);

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

  const handleOpenChange = (nextOpen: boolean) => {
    onOpenChange(nextOpen);
    if (!nextOpen) {
      const store = useProfileStore.getState();
      const activeWorlds = useMudStore.getState().worlds;
      for (const wname in activeWorlds) {
        const connWorld = activeWorlds[wname];
        if (connWorld.connected) {
          const profile = store.worlds.find(
            (wp) => store.worldNameFromUrl(wp.url) === wname
          );
          if (profile) {
            syncWorldMacros(client, profile, wname);
            rescheduleTimers(client, wname, profile);
          }
        }
      }
    }
  };

  return (
    <Dialog.Root open={open} onOpenChange={handleOpenChange}>
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
                worlds.length === 0 ? (
                  <div className="flex flex-col items-center justify-center h-full p-6 text-center gap-4">
                    <div className="w-12 h-12 rounded-full bg-accent flex items-center justify-center text-primary">
                      <Globe className="w-6 h-6" />
                    </div>
                    <div className="max-w-[280px]">
                      <h3 className="text-sm font-semibold text-foreground mb-1">Create a World</h3>
                      <p className="text-xs text-muted-foreground">
                        Get started by adding a world connection profile to discover, share, and play.
                      </p>
                    </div>
                    <Button onClick={handleAddWorld} size="sm">
                      <Plus className="w-4 h-4 mr-1.5" />
                      Add World Profile
                    </Button>
                  </div>
                ) : (
                  <div className="p-6 flex flex-col h-full">
                    <h3 className="text-xs font-semibold uppercase tracking-widest text-muted-foreground mb-4">Saved Worlds</h3>
                    <div className="flex-1 overflow-y-auto min-h-0 flex flex-col gap-2 scrollbar-thin">
                      {worlds.map((w) => (
                        <div
                          key={w.id}
                          className="flex items-center justify-between p-3 rounded-lg border border-border bg-card hover:bg-secondary/30 transition-colors"
                        >
                          <div className="flex items-center gap-2.5 min-w-0">
                            <Globe className="w-4 h-4 text-muted-foreground flex-shrink-0" />
                            <div className="min-w-0">
                              <p className="text-xs font-semibold text-foreground truncate">{w.name}</p>
                              <p className="text-[10px] text-muted-foreground truncate">{w.url}</p>
                            </div>
                          </div>
                          <div className="flex gap-2">
                            <Button
                              variant="outline"
                              size="sm"
                              className="h-7 text-xs px-2.5"
                              onClick={() => setSelection({ kind: 'world', worldId: w.id })}
                            >
                              Edit
                            </Button>
                            <Button
                              size="sm"
                              className="h-7 text-xs px-2.5"
                              onClick={() => {
                                setSelection({ kind: 'world', worldId: w.id });
                                handleConnect(w.id);
                              }}
                            >
                              Connect
                            </Button>
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                )
              ) : selection.kind === 'world' && selectedWorld ? (
                <WorldForm
                  key={selectedWorld.id}
                  world={selectedWorld}
                  onSave={(patch) => updateWorld(selectedWorld.id, patch)}
                  client={client}
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
                Connection service is not running — please restart the application.
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
            'flex-1 flex items-center gap-1.5 px-1 py-1 text-xs rounded min-w-0 transition-colors cursor-pointer focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-primary/12',
            isWorldSelected
              ? 'bg-accent text-accent-foreground'
              : 'text-foreground hover:bg-secondary',
          )}
        >
          <Globe className="w-3 h-3 flex-shrink-0 opacity-60" />
          <span className="truncate font-medium">{world.name}</span>
        </button>
        {/* Add character — appears on hover */}
        <button
          onClick={onAddChar}
          className="opacity-0 group-hover:opacity-100 flex-shrink-0 p-1 text-muted-foreground hover:text-foreground transition-all cursor-pointer focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-primary/12 rounded"
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
                'flex items-center gap-1.5 w-full pl-8 pr-3 py-1 text-xs rounded transition-colors cursor-pointer focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-primary/12',
                isCharSelected
                  ? 'bg-accent text-accent-foreground'
                  : 'text-muted-foreground hover:text-foreground hover:bg-secondary',
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
  client,
}: {
  world: WorldProfile;
  onSave: (patch: Partial<WorldProfile>) => void;
  client: any;
}) {
  const [activeTab, setActiveTab] = useState<'general' | 'aliases' | 'triggers' | 'timers' | 'keys' | 'scripts' | 'variables' | 'safety'>('general');
  
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
    <div className="flex flex-col h-full">
      {/* Tabs Header */}
      <div className="flex border-b border-border bg-muted/10 px-4 py-1 gap-1 flex-shrink-0 select-none">
        <button
          onClick={() => setActiveTab('general')}
          className={cn(
            "px-3 py-1.5 text-[11px] font-medium border-b-2 border-transparent text-muted-foreground hover:text-foreground cursor-pointer transition-all",
            activeTab === 'general' && "border-primary text-foreground font-semibold"
          )}
        >
          General
        </button>
        <button
          onClick={() => setActiveTab('aliases')}
          className={cn(
            "px-3 py-1.5 text-[11px] font-medium border-b-2 border-transparent text-muted-foreground hover:text-foreground cursor-pointer transition-all",
            activeTab === 'aliases' && "border-primary text-foreground font-semibold"
          )}
        >
          Aliases
        </button>
        <button
          onClick={() => setActiveTab('triggers')}
          className={cn(
            "px-3 py-1.5 text-[11px] font-medium border-b-2 border-transparent text-muted-foreground hover:text-foreground cursor-pointer transition-all",
            activeTab === 'triggers' && "border-primary text-foreground font-semibold"
          )}
        >
          Triggers
        </button>
        <button
          onClick={() => setActiveTab('timers')}
          className={cn(
            "px-3 py-1.5 text-[11px] font-medium border-b-2 border-transparent text-muted-foreground hover:text-foreground cursor-pointer transition-all",
            activeTab === 'timers' && "border-primary text-foreground font-semibold"
          )}
        >
          Timers
        </button>
        <button
          onClick={() => setActiveTab('keys')}
          className={cn(
            "px-3 py-1.5 text-[11px] font-medium border-b-2 border-transparent text-muted-foreground hover:text-foreground cursor-pointer transition-all",
            activeTab === 'keys' && "border-primary text-foreground font-semibold"
          )}
        >
          Keys
        </button>
        <button
          onClick={() => setActiveTab('scripts')}
          className={cn(
            "px-3 py-1.5 text-[11px] font-medium border-b-2 border-transparent text-muted-foreground hover:text-foreground cursor-pointer transition-all",
            activeTab === 'scripts' && "border-primary text-foreground font-semibold"
          )}
        >
          Scripts
        </button>
        <button
          onClick={() => setActiveTab('variables')}
          className={cn(
            "px-3 py-1.5 text-[11px] font-medium border-b-2 border-transparent text-muted-foreground hover:text-foreground cursor-pointer transition-all",
            activeTab === 'variables' && "border-primary text-foreground font-semibold"
          )}
        >
          Variables
        </button>
        <button
          onClick={() => setActiveTab('safety')}
          className={cn(
            "px-3 py-1.5 text-[11px] font-medium border-b-2 border-transparent text-muted-foreground hover:text-foreground cursor-pointer transition-all",
            activeTab === 'safety' && "border-primary text-foreground font-semibold"
          )}
        >
          Safety
        </button>
      </div>

      {/* Tab Contents */}
      <div className="flex-1 min-h-0 overflow-y-auto p-5">
        {activeTab === 'general' && (
          <div className="flex flex-col gap-5">
            <SectionLabel>World Connection</SectionLabel>

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
                className="w-full bg-card border border-border rounded-md px-[14px] py-[10px] text-sm text-foreground focus:outline-none focus:border-primary focus:ring-3 focus:ring-primary/12 cursor-pointer transition-colors"
              >
                <option value="utf-8">UTF-8</option>
                <option value="latin1">Latin-1 (ISO-8859-1)</option>
              </select>
            </FieldGroup>

            <FieldGroup>
              <label className="flex items-center gap-3 cursor-pointer select-none">
                <input
                  type="checkbox"
                  checked={gmcp}
                  onChange={(e) => { setGmcp(e.target.checked); save({ gmcp: e.target.checked }); }}
                  className="w-5 h-5 rounded-full appearance-none border border-border bg-secondary checked:bg-primary checked:border-primary cursor-pointer transition-all duration-200 relative checked:after:content-[''] checked:after:absolute checked:after:left-[7px] checked:after:top-[3px] checked:after:w-[5px] checked:after:h-[10px] checked:after:border-r-2 checked:after:border-b-2 checked:after:border-white checked:after:rotate-45 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-primary/12"
                />
                <span className="text-sm font-medium text-foreground">Enable GMCP</span>
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
        )}

        {activeTab === 'aliases' && (
          <AliasesPanel world={world} onSave={save} />
        )}

        {activeTab === 'triggers' && (
          <TriggersPanel world={world} onSave={save} />
        )}

        {activeTab === 'timers' && (
          <TimersPanel world={world} onSave={save} />
        )}

        {activeTab === 'keys' && (
          <KeybindingsPanel world={world} onSave={save} />
        )}

        {activeTab === 'scripts' && (
          <ScriptsPanel world={world} onSave={save} client={client} />
        )}

        {activeTab === 'variables' && (
          <VariablesPanel world={world} onSave={save} />
        )}

        {activeTab === 'safety' && (
          <SafetyPanel world={world} onSave={save} />
        )}
      </div>
    </div>
  );
}

// ── Aliases Panel ───────────────────────────────────────────────────────────

function AliasesPanel({ world, onSave }: { world: WorldProfile; onSave: (patch: Partial<WorldProfile>) => void }) {
  const aliases = world.aliases ?? [];
  const handleAdd = () => {
    const newAlias = { id: crypto.randomUUID(), pattern: '', body: '', enabled: true };
    onSave({ aliases: [...aliases, newAlias] });
  };
  const handleUpdate = (id: string, patch: Partial<AliasProfile>) => {
    onSave({
      aliases: aliases.map((a) => (a.id === id ? { ...a, ...patch } : a)),
    });
  };
  const handleDelete = (id: string) => {
    onSave({ aliases: aliases.filter((a) => a.id !== id) });
  };

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between flex-shrink-0">
        <SectionLabel>Aliases</SectionLabel>
        <Button onClick={handleAdd} size="sm" variant="outline" className="h-7 px-2.5 text-xs">
          <Plus className="w-3.5 h-3.5 mr-1" /> Add Alias
        </Button>
      </div>
      <div className="flex flex-col gap-3 max-h-[300px] overflow-y-auto pr-1 scrollbar-thin flex-1 min-h-0">
        {aliases.length === 0 ? (
          <p className="text-xs text-muted-foreground italic">No aliases defined yet.</p>
        ) : (
          aliases.map((alias) => (
            <div key={alias.id} className="p-3 border border-border rounded-lg bg-card/50 flex flex-col gap-2 relative">
              <div className="flex items-center justify-between gap-3">
                <label className="flex items-center gap-2 cursor-pointer select-none">
                  <input
                    type="checkbox"
                    checked={alias.enabled}
                    onChange={(e) => handleUpdate(alias.id, { enabled: e.target.checked })}
                    className="w-4 h-4 rounded-full appearance-none border border-border bg-secondary checked:bg-primary checked:border-primary cursor-pointer transition-all duration-200 relative checked:after:content-[''] checked:after:absolute checked:after:left-[5px] checked:after:top-[2px] checked:after:w-[4px] checked:after:h-[8px] checked:after:border-r-2 checked:after:border-b-2 checked:after:border-white checked:after:rotate-45"
                  />
                  <span className="text-[11px] font-medium text-muted-foreground">Enabled</span>
                </label>
                <button
                  onClick={() => handleDelete(alias.id)}
                  className="text-muted-foreground hover:text-destructive transition-colors p-1 rounded hover:bg-muted/50 cursor-pointer"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </button>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <FieldGroup>
                  <Label>Alias Word</Label>
                  <Input
                    value={alias.pattern}
                    onChange={(v) => handleUpdate(alias.id, { pattern: v })}
                    placeholder="e.g. kk"
                  />
                </FieldGroup>
                <FieldGroup>
                  <Label>Commands</Label>
                  <Input
                    value={alias.body}
                    onChange={(v) => handleUpdate(alias.id, { body: v })}
                    placeholder="e.g. kill target"
                  />
                </FieldGroup>
              </div>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

// ── Triggers Panel ──────────────────────────────────────────────────────────

function TriggersPanel({ world, onSave }: { world: WorldProfile; onSave: (patch: Partial<WorldProfile>) => void }) {
  const triggers = world.triggers ?? [];
  const handleAdd = () => {
    const newTrigger = {
      id: crypto.randomUUID(),
      pattern: '',
      matchMode: 'regexp' as const,
      type: 'trigger' as const,
      body: '',
      priority: 50,
      enabled: true,
    };
    onSave({ triggers: [...triggers, newTrigger] });
  };
  const handleUpdate = (id: string, patch: Partial<TriggerProfile>) => {
    onSave({
      triggers: triggers.map((t) => (t.id === id ? { ...t, ...patch } : t)),
    });
  };
  const handleDelete = (id: string) => {
    onSave({ triggers: triggers.filter((t) => t.id !== id) });
  };

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between flex-shrink-0">
        <SectionLabel>Triggers</SectionLabel>
        <Button onClick={handleAdd} size="sm" variant="outline" className="h-7 px-2.5 text-xs">
          <Plus className="w-3.5 h-3.5 mr-1" /> Add Trigger
        </Button>
      </div>
      <div className="flex flex-col gap-3 max-h-[300px] overflow-y-auto pr-1 scrollbar-thin flex-1 min-h-0">
        {triggers.length === 0 ? (
          <p className="text-xs text-muted-foreground italic">No triggers defined yet.</p>
        ) : (
          triggers.map((trg) => (
            <div key={trg.id} className="p-3 border border-border rounded-lg bg-card/50 flex flex-col gap-3 relative">
              <div className="flex items-center justify-between gap-3">
                <div className="flex items-center gap-3">
                  <label className="flex items-center gap-2 cursor-pointer select-none">
                    <input
                      type="checkbox"
                      checked={trg.enabled}
                      onChange={(e) => handleUpdate(trg.id, { enabled: e.target.checked })}
                      className="w-4 h-4 rounded-full appearance-none border border-border bg-secondary checked:bg-primary checked:border-primary cursor-pointer transition-all duration-200 relative checked:after:content-[''] checked:after:absolute checked:after:left-[5px] checked:after:top-[2px] checked:after:w-[4px] checked:after:h-[8px] checked:after:border-r-2 checked:after:border-b-2 checked:after:border-white checked:after:rotate-45"
                    />
                    <span className="text-[11px] font-medium text-muted-foreground">Enabled</span>
                  </label>
                  <div className="flex items-center gap-1">
                    <span className="text-[10px] text-muted-foreground font-medium">Priority</span>
                    <input
                      type="number"
                      value={trg.priority}
                      onChange={(e) => handleUpdate(trg.id, { priority: parseInt(e.target.value) || 0 })}
                      className="w-12 bg-card border border-border rounded px-1.5 py-0.5 text-xs text-foreground focus:outline-none focus:border-primary"
                    />
                  </div>
                </div>
                <button
                  onClick={() => handleDelete(trg.id)}
                  className="text-muted-foreground hover:text-destructive transition-colors p-1 rounded hover:bg-muted/50 cursor-pointer"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </button>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <FieldGroup>
                  <Label>Match Pattern</Label>
                  <Input
                    value={trg.pattern}
                    onChange={(v) => handleUpdate(trg.id, { pattern: v })}
                    placeholder="e.g. ^You feel better"
                  />
                </FieldGroup>
                <div className="grid grid-cols-2 gap-2">
                  <FieldGroup>
                    <Label>Match Mode</Label>
                    <select
                      value={trg.matchMode}
                      onChange={(e) => handleUpdate(trg.id, { matchMode: e.target.value as any })}
                      className="w-full bg-card border border-border rounded-md px-2.5 py-2 text-xs text-foreground focus:outline-none focus:border-primary cursor-pointer"
                    >
                      <option value="regexp">Regex</option>
                      <option value="glob">Glob</option>
                      <option value="substr">Literal</option>
                    </select>
                  </FieldGroup>
                  <FieldGroup>
                    <Label>Action Type</Label>
                    <select
                      value={trg.type}
                      onChange={(e) => handleUpdate(trg.id, { type: e.target.value as any })}
                      className="w-full bg-card border border-border rounded-md px-2.5 py-2 text-xs text-foreground focus:outline-none focus:border-primary cursor-pointer"
                    >
                      <option value="trigger">Trigger (Cmd)</option>
                      <option value="gag">Gag (Hide)</option>
                      <option value="hilite">Highlight</option>
                      <option value="substitute">Substitute</option>
                    </select>
                  </FieldGroup>
                </div>
              </div>
              {trg.type !== 'gag' && (
                <FieldGroup>
                  <Label>
                    {trg.type === 'trigger' && 'Commands to run'}
                    {trg.type === 'hilite' && 'Style/Color (e.g. red, bold, yellow)'}
                    {trg.type === 'substitute' && 'Replacement Text'}
                  </Label>
                  <Input
                    value={trg.body}
                    onChange={(v) => handleUpdate(trg.id, { body: v })}
                    placeholder={
                      trg.type === 'trigger' ? 'e.g. stand; /echo healed!' :
                      trg.type === 'hilite' ? 'e.g. red' :
                      'e.g. [HEALTHY] You feel great!'
                    }
                  />
                </FieldGroup>
              )}
            </div>
          ))
        )}
      </div>
    </div>
  );
}

// ── Timers Panel ────────────────────────────────────────────────────────────

function TimersPanel({ world, onSave }: { world: WorldProfile; onSave: (patch: Partial<WorldProfile>) => void }) {
  const timers = world.timers ?? [];
  const handleAdd = () => {
    const newTimer = { id: crypto.randomUUID(), duration: '10s', repeat: true, body: '', enabled: true };
    onSave({ timers: [...timers, newTimer] });
  };
  const handleUpdate = (id: string, patch: Partial<TimerProfile>) => {
    onSave({
      timers: timers.map((t) => (t.id === id ? { ...t, ...patch } : t)),
    });
  };
  const handleDelete = (id: string) => {
    onSave({ timers: timers.filter((t) => t.id !== id) });
  };

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between flex-shrink-0">
        <SectionLabel>Interval Timers</SectionLabel>
        <Button onClick={handleAdd} size="sm" variant="outline" className="h-7 px-2.5 text-xs">
          <Plus className="w-3.5 h-3.5 mr-1" /> Add Timer
        </Button>
      </div>
      <div className="flex flex-col gap-3 max-h-[300px] overflow-y-auto pr-1 scrollbar-thin flex-1 min-h-0">
        {timers.length === 0 ? (
          <p className="text-xs text-muted-foreground italic">No timers defined yet.</p>
        ) : (
          timers.map((timer) => (
            <div key={timer.id} className="p-3 border border-border rounded-lg bg-card/50 flex flex-col gap-2 relative">
              <div className="flex items-center justify-between gap-3">
                <div className="flex items-center gap-4">
                  <label className="flex items-center gap-2 cursor-pointer select-none">
                    <input
                      type="checkbox"
                      checked={timer.enabled}
                      onChange={(e) => handleUpdate(timer.id, { enabled: e.target.checked })}
                      className="w-4 h-4 rounded-full appearance-none border border-border bg-secondary checked:bg-primary checked:border-primary cursor-pointer transition-all duration-200 relative checked:after:content-[''] checked:after:absolute checked:after:left-[5px] checked:after:top-[2px] checked:after:w-[4px] checked:after:h-[8px] checked:after:border-r-2 checked:after:border-b-2 checked:after:border-white checked:after:rotate-45"
                    />
                    <span className="text-[11px] font-medium text-muted-foreground">Enabled</span>
                  </label>
                  <label className="flex items-center gap-2 cursor-pointer select-none">
                    <input
                      type="checkbox"
                      checked={timer.repeat}
                      onChange={(e) => handleUpdate(timer.id, { repeat: e.target.checked })}
                      className="w-4 h-4 rounded appearance-none border border-border bg-secondary checked:bg-primary checked:border-primary cursor-pointer transition-all duration-200 relative checked:after:content-[''] checked:after:absolute checked:after:left-[4px] checked:after:top-[1px] checked:after:w-[4px] checked:after:h-[8px] checked:after:border-r-2 checked:after:border-b-2 checked:after:border-white checked:after:rotate-45"
                    />
                    <span className="text-[11px] font-medium text-muted-foreground">Repeat</span>
                  </label>
                </div>
                <button
                  onClick={() => handleDelete(timer.id)}
                  className="text-muted-foreground hover:text-destructive transition-colors p-1 rounded hover:bg-muted/50 cursor-pointer"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </button>
              </div>
              <div className="grid grid-cols-3 gap-3">
                <div className="col-span-1">
                  <FieldGroup>
                    <Label>Duration</Label>
                    <Input
                      value={timer.duration}
                      onChange={(v) => handleUpdate(timer.id, { duration: v })}
                      placeholder="e.g. 5s or 1m"
                    />
                  </FieldGroup>
                </div>
                <div className="col-span-2">
                  <FieldGroup>
                    <Label>Command / Action</Label>
                    <Input
                      value={timer.body}
                      onChange={(v) => handleUpdate(timer.id, { body: v })}
                      placeholder="e.g. look or /say alive!"
                    />
                  </FieldGroup>
                </div>
              </div>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

// ── Keybindings Panel ───────────────────────────────────────────────────────

function KeybindingsPanel({ world, onSave }: { world: WorldProfile; onSave: (patch: Partial<WorldProfile>) => void }) {
  const kbs = world.keybindings ?? [];
  const handleAdd = () => {
    const newKb = { id: crypto.randomUUID(), key: '', body: '', enabled: true };
    onSave({ keybindings: [...kbs, newKb] });
  };
  const handleUpdate = (id: string, patch: Partial<KeyBindingProfile>) => {
    onSave({
      keybindings: kbs.map((k) => (k.id === id ? { ...k, ...patch } : k)),
    });
  };
  const handleDelete = (id: string) => {
    onSave({ keybindings: kbs.filter((k) => k.id !== id) });
  };

  const handleShortcutKeyDown = (id: string, e: React.KeyboardEvent<HTMLInputElement>) => {
    e.preventDefault();
    e.stopPropagation();

    const parts: string[] = [];
    if (e.ctrlKey) parts.push('ctrl');
    if (e.altKey) parts.push('alt');
    if (e.shiftKey) parts.push('shift');
    if (e.metaKey) parts.push('meta');

    let keyName = e.key.toLowerCase();
    if (keyName === 'arrowup') keyName = 'up';
    else if (keyName === 'arrowdown') keyName = 'down';
    else if (keyName === 'arrowleft') keyName = 'left';
    else if (keyName === 'arrowright') keyName = 'right';
    else if (keyName === ' ') keyName = 'space';

    if (!['control', 'alt', 'shift', 'meta'].includes(keyName)) {
      parts.push(keyName);
    }

    if (parts.length > 0) {
      const combo = parts.join('+');
      handleUpdate(id, { key: combo });
    }
  };

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between flex-shrink-0">
        <SectionLabel>Key Bindings</SectionLabel>
        <Button onClick={handleAdd} size="sm" variant="outline" className="h-7 px-2.5 text-xs">
          <Plus className="w-3.5 h-3.5 mr-1" /> Add Keybinding
        </Button>
      </div>
      <div className="flex flex-col gap-3 max-h-[300px] overflow-y-auto pr-1 scrollbar-thin flex-1 min-h-0">
        {kbs.length === 0 ? (
          <p className="text-xs text-muted-foreground italic">No keybindings defined yet.</p>
        ) : (
          kbs.map((kb) => (
            <div key={kb.id} className="p-3 border border-border rounded-lg bg-card/50 flex flex-col gap-2 relative">
              <div className="flex items-center justify-between gap-3">
                <label className="flex items-center gap-2 cursor-pointer select-none">
                  <input
                    type="checkbox"
                    checked={kb.enabled}
                    onChange={(e) => handleUpdate(kb.id, { enabled: e.target.checked })}
                    className="w-4 h-4 rounded-full appearance-none border border-border bg-secondary checked:bg-primary checked:border-primary cursor-pointer transition-all duration-200 relative checked:after:content-[''] checked:after:absolute checked:after:left-[5px] checked:after:top-[2px] checked:after:w-[4px] checked:after:h-[8px] checked:after:border-r-2 checked:after:border-b-2 checked:after:border-white checked:after:rotate-45"
                  />
                  <span className="text-[11px] font-medium text-muted-foreground">Enabled</span>
                </label>
                <button
                  onClick={() => handleDelete(kb.id)}
                  className="text-muted-foreground hover:text-destructive transition-colors p-1 rounded hover:bg-muted/50 cursor-pointer"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </button>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <FieldGroup>
                  <Label>Shortcut Key</Label>
                  <input
                    type="text"
                    value={kb.key}
                    onKeyDown={(e) => handleShortcutKeyDown(kb.id, e)}
                    placeholder="Focus & press key combo"
                    className="w-full bg-card border border-border rounded-md px-[14px] py-[10px] text-sm text-foreground focus:outline-none focus:border-primary focus:ring-3 focus:ring-primary/12 font-mono"
                    readOnly
                  />
                </FieldGroup>
                <FieldGroup>
                  <Label>MUD Command / Macro</Label>
                  <Input
                    value={kb.body}
                    onChange={(v) => handleUpdate(kb.id, { body: v })}
                    placeholder="e.g. north or /say run!"
                  />
                </FieldGroup>
              </div>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

// ── Scripts Panel ───────────────────────────────────────────────────────────

const EMPTY_LINES: any[] = [];

function ScriptsPanel({
  world,
  onSave,
  client,
}: {
  world: WorldProfile;
  onSave: (patch: Partial<WorldProfile>) => void;
  client: any;
}) {
  const [jsPath, setJsPath] = useState(world.jsScriptPath ?? '');
  const [pyPath, setPyPath] = useState(world.pyScriptPath ?? '');
  const [tfPath, setTfPath] = useState(world.tfScriptPath ?? '');
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [successMsg, setSuccessMsg] = useState<string | null>(null);

  const localLines = useMudStore((s) => s.worlds['local']?.lines) ?? EMPTY_LINES;
  const logLines = localLines.slice(-100);

  const handleReloadJS = async () => {
    setErrorMsg(null);
    setSuccessMsg(null);
    if (!jsPath.trim()) {
      setErrorMsg('Please enter a JavaScript file path first.');
      return;
    }
    try {
      await client.cmd(`/js ${jsPath}`);
      setSuccessMsg('JavaScript script loaded successfully.');
    } catch (e: any) {
      setErrorMsg(e.message || 'Failed to load JavaScript script.');
    }
  };

  const handleReloadPy = async () => {
    setErrorMsg(null);
    setSuccessMsg(null);
    if (!pyPath.trim()) {
      setErrorMsg('Please enter a Python file path first.');
      return;
    }
    try {
      await client.cmd(`/py ${pyPath}`);
      setSuccessMsg('Python script loaded successfully.');
    } catch (e: any) {
      setErrorMsg(e.message || 'Failed to load Python script.');
    }
  };

  const handleReloadTF = async () => {
    setErrorMsg(null);
    setSuccessMsg(null);
    if (!tfPath.trim()) {
      setErrorMsg('Please enter a TinyFugue (.tf) file path first.');
      return;
    }
    try {
      await client.cmd(`/load ${tfPath}`);
      setSuccessMsg('TinyFugue script loaded successfully.');
    } catch (e: any) {
      setErrorMsg(e.message || 'Failed to load TinyFugue script.');
    }
  };

  return (
    <div className="flex flex-col gap-4 h-full">
      <SectionLabel>Scripting Sidecar</SectionLabel>

      {errorMsg && <p className="text-xs text-destructive flex-shrink-0">{errorMsg}</p>}
      {successMsg && <p className="text-xs text-success flex-shrink-0">{successMsg}</p>}

      <div className="grid grid-cols-2 gap-4 flex-shrink-0">
        {/* JS Sidecar */}
        <div className="p-3 border border-border rounded-lg bg-card/30 flex flex-col gap-3">
          <label className="flex items-center gap-2 cursor-pointer select-none">
            <input
              type="checkbox"
              checked={world.jsScriptEnabled}
              onChange={(e) => onSave({ jsScriptEnabled: e.target.checked })}
              className="w-4 h-4 rounded-full appearance-none border border-border bg-secondary checked:bg-primary checked:border-primary cursor-pointer transition-all duration-200 relative checked:after:content-[''] checked:after:absolute checked:after:left-[5px] checked:after:top-[2px] checked:after:w-[4px] checked:after:h-[8px] checked:after:border-r-2 checked:after:border-b-2 checked:after:border-white checked:after:rotate-45"
            />
            <span className="text-xs font-semibold text-foreground">JavaScript Sidecar</span>
          </label>
          <FieldGroup>
            <Label>Script File Path</Label>
            <div className="flex gap-2">
              <Input
                value={jsPath}
                onChange={(v) => { setJsPath(v); onSave({ jsScriptPath: v }); }}
                placeholder="scripts/my_script.js"
              />
              <Button onClick={handleReloadJS} size="sm" variant="outline" className="h-[38px] px-3">Reload</Button>
            </div>
          </FieldGroup>
        </div>

        {/* Python Sidecar */}
        <div className="p-3 border border-border rounded-lg bg-card/30 flex flex-col gap-3">
          <label className="flex items-center gap-2 cursor-pointer select-none">
            <input
              type="checkbox"
              checked={world.pyScriptEnabled}
              onChange={(e) => onSave({ pyScriptEnabled: e.target.checked })}
              className="w-4 h-4 rounded-full appearance-none border border-border bg-secondary checked:bg-primary checked:border-primary cursor-pointer transition-all duration-200 relative checked:after:content-[''] checked:after:absolute checked:after:left-[5px] checked:after:top-[2px] checked:after:w-[4px] checked:after:h-[8px] checked:after:border-r-2 checked:after:border-b-2 checked:after:border-white checked:after:rotate-45"
            />
            <span className="text-xs font-semibold text-foreground">Python Sidecar</span>
          </label>
          <FieldGroup>
            <Label>Script File Path</Label>
            <div className="flex gap-2">
              <Input
                value={pyPath}
                onChange={(v) => { setPyPath(v); onSave({ pyScriptPath: v }); }}
                placeholder="scripts/my_script.py"
              />
              <Button onClick={handleReloadPy} size="sm" variant="outline" className="h-[38px] px-3">Reload</Button>
            </div>
          </FieldGroup>
        </div>
      </div>

      {/* TinyFugue (.tf) Loader */}
      <div className="p-3 border border-border rounded-lg bg-card/30 flex flex-col gap-3 flex-shrink-0">
        <label className="flex items-center gap-2 cursor-pointer select-none">
          <input
            type="checkbox"
            checked={world.tfScriptEnabled}
            onChange={(e) => onSave({ tfScriptEnabled: e.target.checked })}
            className="w-4 h-4 rounded-full appearance-none border border-border bg-secondary checked:bg-primary checked:border-primary cursor-pointer transition-all duration-200 relative checked:after:content-[''] checked:after:absolute checked:after:left-[5px] checked:after:top-[2px] checked:after:w-[4px] checked:after:h-[8px] checked:after:border-r-2 checked:after:border-b-2 checked:after:border-white checked:after:rotate-45"
          />
          <span className="text-xs font-semibold text-foreground">TinyFugue Script (.tf)</span>
        </label>
        <FieldGroup>
          <Label>Script File Path</Label>
          <div className="flex gap-2">
            <Input
              value={tfPath}
              onChange={(v) => { setTfPath(v); onSave({ tfScriptPath: v }); }}
              placeholder="scripts/my_macros.tf"
            />
            <Button onClick={handleReloadTF} size="sm" variant="outline" className="h-[38px] px-3">Load</Button>
          </div>
        </FieldGroup>
        <p className="text-[11px] text-muted-foreground/60">Loads a TinyFugue-compatible macro file via <span className="font-mono">/load</span>.</p>
      </div>

      {/* Runtime Logging Console */}
      <div className="flex flex-col gap-1.5 flex-1 min-h-0">
        <Label>Runtime Logging Console (system & scripts)</Label>
        <div className="border border-border bg-background/50 rounded-lg p-2.5 h-[100px] overflow-y-auto font-mono text-[11px] leading-relaxed scrollbar-thin">
          {logLines.length === 0 ? (
            <span className="text-muted-foreground/60 italic">No output logs recorded.</span>
          ) : (
            logLines.map((line, idx) => (
              <div key={idx} className="whitespace-pre-wrap select-text text-muted-foreground">
                <span className="text-muted-foreground/30 mr-1.5">[{new Date().toLocaleTimeString()}]</span>
                {line.Text}
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
}

// ── Variables Panel ──────────────────────────────────────────────────────────

function VariablesPanel({ world, onSave }: { world: WorldProfile; onSave: (patch: Partial<WorldProfile>) => void }) {
  const vars = world.variables ?? [];

  const handleAdd = () => {
    const newVar: VariableProfile = { id: crypto.randomUUID(), name: '', value: '', enabled: true };
    onSave({ variables: [...vars, newVar] });
  };
  const handleUpdate = (id: string, patch: Partial<VariableProfile>) => {
    onSave({ variables: vars.map((v) => (v.id === id ? { ...v, ...patch } : v)) });
  };
  const handleDelete = (id: string) => {
    onSave({ variables: vars.filter((v) => v.id !== id) });
  };

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between flex-shrink-0">
        <SectionLabel>Global Variables</SectionLabel>
        <Button onClick={handleAdd} size="sm" variant="outline" className="h-7 px-2.5 text-xs">
          <Plus className="w-3.5 h-3.5 mr-1" /> Add Variable
        </Button>
      </div>
      <p className="text-[11px] text-muted-foreground/70">
        Variables are set in the backend via <span className="font-mono">/set name=value</span> when you connect.
      </p>
      <div className="flex flex-col gap-3 max-h-[300px] overflow-y-auto pr-1 scrollbar-thin flex-1 min-h-0">
        {vars.length === 0 ? (
          <p className="text-xs text-muted-foreground italic">No variables defined yet.</p>
        ) : (
          vars.map((v) => (
            <div key={v.id} className="p-3 border border-border rounded-lg bg-card/50 flex flex-col gap-2" data-testid="variable-card">
              <div className="flex items-center justify-between gap-3">
                <label className="flex items-center gap-2 cursor-pointer select-none">
                  <input
                    type="checkbox"
                    checked={v.enabled}
                    onChange={(e) => handleUpdate(v.id, { enabled: e.target.checked })}
                    className="w-4 h-4 rounded-full appearance-none border border-border bg-secondary checked:bg-primary checked:border-primary cursor-pointer transition-all duration-200 relative checked:after:content-[''] checked:after:absolute checked:after:left-[5px] checked:after:top-[2px] checked:after:w-[4px] checked:after:h-[8px] checked:after:border-r-2 checked:after:border-b-2 checked:after:border-white checked:after:rotate-45"
                  />
                  <span className="text-[11px] font-medium text-muted-foreground">Enabled</span>
                </label>
                <button
                  onClick={() => handleDelete(v.id)}
                  className="text-muted-foreground hover:text-destructive transition-colors p-1 rounded hover:bg-muted/50 cursor-pointer"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </button>
              </div>
              <div className="grid grid-cols-2 gap-3">
                <FieldGroup>
                  <Label>Name</Label>
                  <Input
                    value={v.name}
                    onChange={(val) => handleUpdate(v.id, { name: val })}
                    placeholder="e.g. target"
                    monospace
                  />
                </FieldGroup>
                <FieldGroup>
                  <Label>Value</Label>
                  <Input
                    value={v.value}
                    onChange={(val) => handleUpdate(v.id, { value: val })}
                    placeholder="e.g. orc"
                    monospace
                  />
                </FieldGroup>
              </div>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

// ── Safety Panel ─────────────────────────────────────────────────────────────

function SafetyPanel({ world, onSave }: { world: WorldProfile; onSave: (patch: Partial<WorldProfile>) => void }) {
  const restrictions = [
    {
      key: 'restrictShell' as const,
      label: 'Disallow Shell Execution (SHELL)',
      hint: 'Prevents /js and /py scripts from running. Applied via /restrict SHELL.',
      value: world.restrictShell ?? false,
    },
    {
      key: 'restrictFile' as const,
      label: 'Disallow File Access (FILE)',
      hint: 'Prevents /log and /load from reading or writing files. Applied via /restrict FILE.',
      value: world.restrictFile ?? false,
    },
    {
      key: 'restrictWorld' as const,
      label: 'Disallow External Connections (WORLD)',
      hint: 'Prevents /connect and /addworld from opening new connections. Applied via /restrict WORLD.',
      value: world.restrictWorld ?? false,
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <SectionLabel>Safety Restrictions</SectionLabel>
      <p className="text-[11px] text-muted-foreground/70">
        Active restrictions are sent to the backend via <span className="font-mono">/restrict CAPABILITY</span> when you connect. Once set, restrictions cannot be unset in the current session.
      </p>
      <div className="flex flex-col gap-3">
        {restrictions.map((r) => (
          <div key={r.key} className="p-3 border border-border rounded-lg bg-card/50 flex flex-col gap-1.5">
            <label className="flex items-center gap-3 cursor-pointer select-none">
              <input
                type="checkbox"
                checked={r.value}
                onChange={(e) => onSave({ [r.key]: e.target.checked })}
                className="w-5 h-5 rounded appearance-none border border-border bg-secondary checked:bg-destructive checked:border-destructive cursor-pointer transition-all duration-200 relative checked:after:content-[''] checked:after:absolute checked:after:left-[7px] checked:after:top-[3px] checked:after:w-[5px] checked:after:h-[10px] checked:after:border-r-2 checked:after:border-b-2 checked:after:border-white checked:after:rotate-45 focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-primary/12"
              />
              <span className="text-sm font-medium text-foreground">{r.label}</span>
            </label>
            <p className="text-[11px] text-muted-foreground/70 pl-8">{r.hint}</p>
          </div>
        ))}
      </div>
    </div>
  );
}


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
        'w-full bg-card border border-border rounded-md px-[14px] py-[10px] text-sm text-foreground transition-colors',
        'placeholder:text-muted-foreground/40',
        'focus:outline-none focus:border-primary focus:ring-3 focus:ring-primary/12',
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
        'w-full bg-card border border-border rounded-md px-[14px] py-[10px] text-sm text-foreground resize-none transition-colors',
        'placeholder:text-muted-foreground/40',
        'focus:outline-none focus:border-primary focus:ring-3 focus:ring-primary/12',
        monospace && 'font-mono',
      )}
    />
  );
}
