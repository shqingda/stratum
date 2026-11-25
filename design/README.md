# Stratum Design — Material You × FlClash "Tonal Strata"

Stratum combines **Google Material You (Material Design 3 Expressive)** structural clarity and **FlClash 0.9.x** self-developed desktop navigation and HCT dynamic color architecture.

## Core Principles

1. **No Apple Glass / Cupertino Chroming**
   - No OS vibrancy, frosted translucent sidebars (`VibrancyGlass`), specular `rim` borders, Safari-style edge blur fades (`ScrollEdge`), or trackpad haptic ticks.
   - Depth is expressed through **5-level opaque tonal surface containers** (`SurfaceContainer` → `SurfaceContainerLow` → `Surface` → `SurfaceContainerHigh` → `SurfaceContainerHighest`), using `OutlineVariant` hairline dividers where structural separation is needed and M3 elevation shadows (`elevation2`/`elevation3`) strictly for floating overlays and dialogs.

2. **Pure Go HCT Dynamic Color Engine (`app/hct.go`)**
   - Single-seed CAM16/HCT tonal palette derivation with **4 FlClash color styles**:
     - `Soft` (default, TonalSpot-inspired calm chroma)
     - `Vivid` (high chroma primary/secondary/tertiary)
     - `Tinted` (seed-chroma washed surfaces)
     - `Monochrome` (achromatic editorial ink & slate surfaces)
   - Default seed: **Cobalt Basalt (`#3B6698`)**, alongside 5 mineral presets (Malachite, Ochre, Garnet, Feldspar, Obsidian) and custom `#RRGGBB` input, plus **Pure Black OLED (`#000000`)** dark surface mode.
   - Live Oklab palette crossfade transitions (`PaletteLerpBetween`, `animColor`).

3. **Persistent Navigation Rail & Adaptive Three-Pane Stage**
   - **68dp Navigation Rail (`app/rail.go`)**: Top New Session FAB (`44×44dp`, `Radius 14dp`), `⌘K` search trigger, and 4 primary destinations (`Sessions`, `Automations`, `Extensions`, `Settings`) with `52×32dp` `SecondaryContainer` active pill indicators.
   - **Tonal Strata Stage**: Content destinations mount inside an opaque `16dp` rounded stage (`p.Surface`) resting on the `p.SurfaceContainer` outer window shell (`TitleBarHeight: 48`, `TitleBarHiddenInset`).
   - **Three-Pane Sessions Workspace**: Collapsible 260dp Session Drawer (`p.SurfaceContainerLow`) with FlClash-style in-bar search & status `FilterChip`s (`All`, `Running`, `Pending`, `Archived`), primary Transcript Stage (`p.Surface`), and optional Supporting Review/Preview Pane with M3 Primary Tabs (`3dp` `Primary` indicator bar) and explicit collapse controls (`Resize` drag clamps width without auto-collapsing).

4. **Chrome / Material You Browser & Embedded WebView**
   - Persistent top tab strip even for a single tab (`+` beside tabs, active tab `p.Surface`, inactive `p.SurfaceContainerHigh`), left-aligned `16dp` pill Omnibox (`p.SurfaceContainerHighest`), and M3 token synchronization to `@pierre/diffs` / `@pierre/trees` (`color-mix(in oklab, ...)`, WCAG 3:1 `:focus-visible`).

5. **Motion & Accessibility**
   - M3 Emphasized cubic-bezier curves (`easeEmphasizedDecelerate`, `easeEmphasizedAccelerate`, `easeStandard`) honoring `ReduceMotion`.
   - WCAG 3:1 `m3Focus` focus rings (`2dp` `Primary`), `4.5:1` minimum text contrast verified across all HCT styles, and destructive confirmation dialogs (`28dp` radius) autofocusing `Cancel`.
