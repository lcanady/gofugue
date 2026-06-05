# Genesis

## Overview
An editorial precision interface design system for GoFugue, a modern MUD/MUSH text-based game client. The aesthetic is quietly confident — bold display typography, generous spacing, and gallery-frame panel surfaces. The mood is professional and modern without being sterile, balancing the high density of MUD terminal output with clean, structural breathing room.

## Colors
- **Primary** (#6366F1): Active tabs, primary buttons, input caret-color, focus rings, interactive highlights — indigo
- **Primary Hover** (#818CF8): Brighter indigo for hover states on primary elements in dark mode
- **Background** (#0F0F12): Main window and terminal canvas background, premium dark gray/black
- **Surface** (#17171C): Dialog cards, panels, layout headers, dropdowns
- **Text Primary** (#F2F2F5): Headings, active menu text, labels, primary terminal text — near-white
- **Text Secondary** (#A1A1AA): Descriptions, metadata, secondary labels, muted terminal output
- **Border** (#2A2A30): Panel dividers, tab borders, input borders — subtle and recessive
- **Success** (#10B981): Connected status, successful commands, positive indicators
- **Warning** (#F59E0B): Reconnecting status, caution banners
- **Error** (#EF4444): Disconnected status, validation errors, destructive actions
- **Accent** (239 84% 15%): Dark indigo tint for list item selection / active highlights
- **Accent Foreground** (239 84% 67%): Indigo text on active lists/tree highlights

## Typography
- **Display Font**: General Sans — loaded from Fontshare
- **Body Font**: DM Sans — loaded from Google Fonts
- **Code Font**: JetBrains Mono — loaded from Google Fonts

Display and heading text uses General Sans at bold weight with tight letter spacing (-0.03em to -0.04em). Body and UI text uses DM Sans at regular and medium weights. The contrast between the geometric display font and the humanist body font creates a refined editorial feel. MUD terminal output, command input bars, status bar telemetry, and macro code blocks use JetBrains Mono at regular weight.

Type scale: Display 72px, Headline 60px, Section heading 32px, Subhead 24px, Body 15px, Small 13px, Caption 12px, Overline 11px uppercase.

## Elevation
This design uses minimal shadows. Panels and tabs rest flat with a 1px border. Dropdowns, popovers, and dialog modals use shadow-lg or shadow-2xl. Focus states use a 3px indigo ring (0 0 0 3px rgba(99,102,241,0.12)) rather than a shadow.

## Components
- **Buttons**: Primary uses indigo fill with white text, 6px radius, medium weight. Secondary uses transparent bg with 1px border, same radius. Ghost has no border or bg, just text color change on hover. Destructive uses red text with red border. All buttons shift up 1px on hover. Sizes: small (32px), medium (38px), large (44px).
- **Dashboard / Saved World Cards**: List of connection profiles on the dashboard. Card background uses surface, 1px subtle border, 12px radius. Flex layout with name, URL, and Connect/Edit actions. Hover applies a subtle background change.
- **Inputs & Textareas**: 1px subtle border, card background, 6px radius, 10px vertical and 14px horizontal padding, 14px font size. Focus: border turns indigo with a 3px rgba ring. Error: border turns red. Placeholder text uses muted color.
- **Checkboxes**: 20px size, rounded-full, dark gray unchecked, indigo checked with white checkmark. Used as toggle switches for preferences (e.g. GMCP).
- **Lists**: Stacked rows with 1px dividers between items (e.g. character profiles). Each row is flex with space-between, 12px vertical and 16px horizontal padding. Hover: subtle background change.
- **Tab Header & Controls**: Sticky layout stacks managed by GoldenLayout, 36px height, 1px bottom border. Logo/tabs left, controls (popout, maximize, close) right. Individual tabs use DM Sans at 13px medium weight. Active tab shows an indigo top border and primary text color.
- **Plus (+) Button**: Custom plus button appended next to the tab headers in layout stacks to quickly spawn the World Manager dialog. Styled with transparent background, matching 36px height, and secondary hover highlights.
- **Status Bar**: A single monospace row at the bottom of the window displaying latency, connection status, active MUD name, and client telemetry.

## Spacing
- Base unit: 4px
- Scale: 4, 8, 12, 16, 20, 24, 32, 40, 48, 64, 80, 96px
- Component padding: small 8x12, medium 10x16, large 12x24
- Section spacing: 32px mobile, 48px tablet, 64px desktop
- Layout grid gap: 4px between panels

## Border Radius
- 4px: Badges, inline code
- 6px: Buttons, inputs, selects, tab controls
- 8px: Dropdowns, context menus, panels
- 12px: Dialog modals, dashboard cards
- 9999px: Avatars, status dots, checkboxes

## Do's and Don'ts
- Do use indigo (#6366F1) only for interactive elements — never for decoration or static text
- Do maintain the 4px spacing grid for all padding, margins, and gaps
- Do use General Sans for headings and DM Sans for body — never swap them
- Do keep dialog modals and cards at 12px radius and buttons/inputs at 6px — don't mix these values
- Do maintain readable contrast for terminal output lines and cursor carets
- Don't use pure black (#000000) or pure white (#FFFFFF) for text — use the defined palette values
- Don't add decorative gradients or illustrations
- Don't use shadows on static elements — reserve shadow elevation for hover and focus states
- Don't use more than two font weights on a single screen
- Don't place more than one primary (filled indigo) button in the same view section
