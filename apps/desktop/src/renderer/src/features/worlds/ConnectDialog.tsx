import { useState, type FormEvent } from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import { X } from 'lucide-react';
import { cn } from '@renderer/lib/utils';
import { Button } from '@renderer/components/ui/button';
import type { GofugueClient } from '@gofugue/ipc';

interface ConnectDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  client: GofugueClient;
}

/**
 * Modal dialog for opening a new world connection.
 * Dispatches /connect via the gofugue cmd IPC method.
 */
export function ConnectDialog({ open, onOpenChange, client }: ConnectDialogProps) {
  const [url, setUrl] = useState('mud://');
  const [name, setName] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      const worldName = name.trim() || url.replace(/^.*:\/\//, '').split(':')[0] || 'world';
      // Use the /connect command which the gofugue cmd dispatcher understands.
      await client.cmd(`/connect ${url.trim()}`, worldName);
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Connection failed');
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-black/60 backdrop-blur-sm data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0" />
        <Dialog.Content
          className={cn(
            'fixed left-1/2 top-1/2 z-50 -translate-x-1/2 -translate-y-1/2',
            'w-full max-w-sm rounded-lg border border-border bg-card p-6 shadow-2xl',
            'data-[state=open]:animate-in data-[state=closed]:animate-out',
            'data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0',
            'data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95',
          )}
        >
          <div className="flex items-center justify-between mb-4">
            <Dialog.Title className="text-base font-semibold text-foreground">
              New Connection
            </Dialog.Title>
            <Dialog.Close asChild>
              <button className="text-muted-foreground hover:text-foreground cursor-pointer">
                <X className="w-4 h-4" />
              </button>
            </Dialog.Close>
          </div>

          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-1.5">
              <label className="text-xs text-muted-foreground" htmlFor="conn-url">
                URL
              </label>
              <input
                id="conn-url"
                type="text"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="mud://mymush.org:4201"
                autoFocus
                className={cn(
                  'w-full rounded-md border border-input bg-background px-3 py-2',
                  'text-sm font-mono text-foreground',
                  'placeholder:text-muted-foreground/50',
                  'focus:outline-none focus:ring-1 focus:ring-ring',
                )}
              />
              <p className="text-[11px] text-muted-foreground">
                Schemes: mud:// muds:// ws:// wss://
              </p>
            </div>

            <div className="space-y-1.5">
              <label className="text-xs text-muted-foreground" htmlFor="conn-name">
                Display name <span className="text-muted-foreground/60">(optional)</span>
              </label>
              <input
                id="conn-name"
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="My MUSH"
                className={cn(
                  'w-full rounded-md border border-input bg-background px-3 py-2',
                  'text-sm text-foreground',
                  'placeholder:text-muted-foreground/50',
                  'focus:outline-none focus:ring-1 focus:ring-ring',
                )}
              />
            </div>

            {error && (
              <p className="text-xs text-destructive" role="alert">
                {error}
              </p>
            )}

            <div className="flex justify-end gap-2 pt-1">
              <Dialog.Close asChild>
                <Button type="button" variant="ghost" size="sm">
                  Cancel
                </Button>
              </Dialog.Close>
              <Button type="submit" size="sm" disabled={!url.trim() || loading}>
                {loading ? 'Connecting…' : 'Connect'}
              </Button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
