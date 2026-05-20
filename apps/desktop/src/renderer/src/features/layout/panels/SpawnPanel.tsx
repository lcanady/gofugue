/**
 * SpawnPanel — filtered world output.
 * Shows lines from a specific world that match a regex filter.
 * Full implementation coming; this stub shows a placeholder.
 */

interface Props {
  worldName: string;
  filter: string;
  label: string;
}

export function SpawnPanel({ worldName, filter, label }: Props) {
  return (
    <div className="flex-1 flex items-center justify-center text-muted-foreground text-xs font-mono h-full">
      <div className="text-center space-y-1">
        <div className="opacity-50">spawn</div>
        <div>{label}</div>
        <div className="opacity-40 text-[10px]">{worldName} · /{filter}/</div>
      </div>
    </div>
  );
}
