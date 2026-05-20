import { TerminalPane } from '@renderer/features/terminal/TerminalPane';
import { TerminalContextMenu } from '@renderer/features/terminal/TerminalContextMenu';
import type { GofugueClient } from '@gofugue/ipc';

interface Props {
  worldName: string;
  client: GofugueClient;
}

export function TerminalPanel({ worldName, client }: Props) {
  return (
    <TerminalContextMenu client={client}>
      <TerminalPane worldName={worldName} className="h-full" />
    </TerminalContextMenu>
  );
}
