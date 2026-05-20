/**
 * ANSI 16-color palette. Matches VSCode's built-in terminal theme, which
 * has good readability on dark backgrounds without being garish.
 *
 * Index mapping:
 *   0-7  = standard colors (black, red, green, yellow, blue, magenta, cyan, white)
 *   8-15 = bright variants
 */
export const ANSI_PALETTE: readonly string[] = [
  '#000000', // 0  black
  '#cd3131', // 1  red
  '#0dbc79', // 2  green
  '#e5e510', // 3  yellow
  '#2472c8', // 4  blue
  '#bc3fbc', // 5  magenta
  '#11a8cd', // 6  cyan
  '#e5e5e5', // 7  white
  '#666666', // 8  bright black  (dark gray)
  '#f14c4c', // 9  bright red
  '#23d18b', // 10 bright green
  '#f5f543', // 11 bright yellow
  '#3b8eea', // 12 bright blue
  '#d670d6', // 13 bright magenta
  '#29b8db', // 14 bright cyan
  '#ffffff', // 15 bright white
] as const;

/** Resolve a gofugue FG/BG value to a CSS color string, or undefined for terminal default. */
export function resolveColor(
  index: number,
  rgb: readonly [number, number, number],
): string | undefined {
  if (index === -1) return undefined; // terminal default — let CSS var handle it
  if (index === -2) return `rgb(${rgb[0]},${rgb[1]},${rgb[2]})`;
  const color = ANSI_PALETTE[index];
  return color ?? undefined;
}
