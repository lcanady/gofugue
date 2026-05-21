import { useState, type FormEvent, useEffect } from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import { X } from 'lucide-react';
import { cn } from '@renderer/lib/utils';
import { Button } from '@renderer/components/ui/button';

interface TriggerEditorDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  initialPattern?: string;
}

export function TriggerEditorDialog({ open, onOpenChange, initialPattern = '' }: TriggerEditorDialogProps) {
  const [pattern, setPattern] = useState(initialPattern);
  const [action, setAction] = useState('');

  useEffect(() => {
    if (open) {
      setPattern(initialPattern);
      setAction('');
    }
  }, [open, initialPattern]);

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    console.log('[GoFugue] Creating trigger:', { pattern, action });
    // TODO: implement actual trigger creation logic when trigger engine is added
    onOpenChange(false);
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
              Create Trigger
            </Dialog.Title>
            <Dialog.Close asChild>
              <button className="text-muted-foreground hover:text-foreground cursor-pointer">
                <X className="w-4 h-4" />
              </button>
            </Dialog.Close>
          </div>

          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-1.5">
              <label className="text-xs text-muted-foreground" htmlFor="trigger-pattern">
                Pattern
              </label>
              <input
                id="trigger-pattern"
                type="text"
                value={pattern}
                onChange={(e) => setPattern(e.target.value)}
                placeholder="^You see (.*) here.$"
                autoFocus
                className={cn(
                  'w-full rounded-md border border-input bg-background px-3 py-2',
                  'text-sm font-mono text-foreground',
                  'placeholder:text-muted-foreground/50',
                  'focus:outline-none focus:ring-1 focus:ring-ring',
                )}
              />
            </div>

            <div className="space-y-1.5">
              <label className="text-xs text-muted-foreground" htmlFor="trigger-action">
                Action
              </label>
              <textarea
                id="trigger-action"
                value={action}
                onChange={(e) => setAction(e.target.value)}
                placeholder="say Hello %1!"
                rows={3}
                className={cn(
                  'w-full rounded-md border border-input bg-background px-3 py-2',
                  'text-sm font-mono text-foreground resize-none',
                  'placeholder:text-muted-foreground/50',
                  'focus:outline-none focus:ring-1 focus:ring-ring',
                )}
              />
            </div>

            <div className="flex justify-end gap-2 pt-1">
              <Dialog.Close asChild>
                <Button type="button" variant="ghost" size="sm">
                  Cancel
                </Button>
              </Dialog.Close>
              <Button type="submit" size="sm" disabled={!pattern.trim()}>
                Save
              </Button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
