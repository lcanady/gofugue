import React, { forwardRef, type CSSProperties } from 'react';
import type { WorldLineEvent, Span, LineAttrs } from '@gofugue/ipc';
import { resolveColor } from './palette.js';

function spanStyle(attrs: LineAttrs): CSSProperties {
  const fg = resolveColor(attrs.FG, attrs.FGRGB);
  const bg = resolveColor(attrs.BG, attrs.BGRGB);

  // Reverse swaps fg/bg.
  const finalFg = attrs.Reverse ? bg : fg;
  const finalBg = attrs.Reverse ? fg : bg;

  return {
    color: finalFg,
    backgroundColor: finalBg,
    fontWeight: attrs.Bold ? 'bold' : undefined,
    fontStyle: attrs.Italic ? 'italic' : undefined,
    textDecoration: attrs.Underline ? 'underline' : undefined,
  };
}

interface SpanProps {
  span: Span;
}

function ANSISpan({ span }: SpanProps) {
  const style = spanStyle(span.Attrs);
  const hasStyle = Object.values(style).some((v) => v !== undefined);
  if (!hasStyle) return <>{span.Text}</>;
  return <span style={style}>{span.Text}</span>;
}

export interface ANSILineProps extends React.HTMLAttributes<HTMLDivElement> {
  /** A rendered WorldLineEvent (post-gag/hilite/substitute). */
  event: WorldLineEvent;
}

/**
 * Renders a single MUD output line as styled React spans.
 *
 * - Uses `event.Spans` for mid-line colour changes.
 * - Falls back to `event.Attrs` (whole-line) when Spans is empty.
 * - Skips rendering entirely when `event.Gagged` is true.
 * - Forwards ref so TanStack Virtual can measure row heights.
 */
export const ANSILine = forwardRef<HTMLDivElement, ANSILineProps>(
  ({ event, className, ...rest }, ref) => {
    if (event.Gagged) return null;

    if (event.Spans && event.Spans.length > 0) {
      return (
        <div ref={ref} className={className} aria-label={event.Text} {...rest}>
          {event.Spans.map((span, i) => (
            <ANSISpan key={i} span={span} />
          ))}
        </div>
      );
    }

    const style = spanStyle(event.Attrs);
    const hasStyle = Object.values(style).some((v) => v !== undefined);
    return (
      <div
        ref={ref}
        className={className}
        style={hasStyle ? style : undefined}
        aria-label={event.Text}
        {...rest}
      >
        {event.Text || '\u00a0'}
      </div>
    );
  },
);
ANSILine.displayName = 'ANSILine';
