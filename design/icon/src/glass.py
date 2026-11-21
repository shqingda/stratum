"""Generates 3 macOS 26 Layered Liquid Glass icon directions for Talos.

Each direction keeps the Talos silhouette (3/4 profile, swept-back crest,
glowing blue ichor neck tube) while removing cartoon outlines and cel-shading
in favor of 3 layered planes:
  1. Background (squircle with depth gradient & rim)
  2. Subject (sculpted bronze / metallic gradients, zero dark outline)
  3. Specular Highlight (Liquid Glass top-left sheen, bevel & rim light)

Produces:
  - talos-glass-{a,b,c}-{light,dark,tinted}.svg  (1024x1024)
  - talos-glass-{a,b,c}-{32,16}.svg             (simplified small sizes)
"""
import math
import os

OUT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))

# Shared 3/4 Talos geometry (refined smooth curves, no cartoon outlines)
HELM = (
    "M524 286 C628 282 700 344 708 430 L714 594 L708 704 "
    "C686 708 656 700 634 686 C604 666 584 644 568 642 "
    "C542 642 520 666 468 694 C436 710 396 716 362 708 "
    "C346 642 336 560 340 478 C346 366 418 290 524 286 Z"
)
HELM_SHADE = (
    "M524 286 C418 290 346 366 340 478 C336 560 346 642 362 708 "
    "C396 716 426 708 456 694 C416 620 404 520 412 430 "
    "C420 356 462 308 524 286 Z"
)
VISOR = (
    "M592 454 C610 442 666 440 722 448 L722 504 L702 506 "
    "L702 652 C695 662 675 662 668 652 L666 506 "
    "C640 508 606 506 594 496 C582 488 582 464 592 454 Z"
)
TORSO = (
    "M144 924 C166 826 244 766 354 748 C426 738 598 738 664 748 "
    "C774 768 852 832 880 924 Z"
)
NECK = (
    "M422 688 C422 728 452 752 518 752 C584 752 618 728 618 688 "
    "L606 632 C576 644 468 644 434 632 Z"
)
VEIN = (
    "M492 648 C482 684 480 718 486 752 L506 752 "
    "C500 718 502 684 512 648 Z"
)


def crest_blades(cx=524, cy=468, n=9, a0=-36, a1=-148, gap=3.4):
    def pol(a, r):
        return cx + r * math.cos(math.radians(a)), cy + r * math.sin(math.radians(a))

    step = (a1 - a0) / n
    out = []
    for i in range(n):
        s0 = a0 + i * step + (gap / 2 if step > 0 else -gap / 2)
        s1 = a0 + (i + 1) * step - (gap / 2 if step > 0 else -gap / 2)
        mid = (i + 0.5) / n
        h = 72 + 124 * math.sin(min(1, mid * 1.25) * math.pi * 0.5) ** 1.4 * (1 - 0.25 * max(0, mid - 0.8) / 0.2)
        lean = step * 1.7
        rin, rout = 188, 188 + h
        pts = [pol(s0, rin), pol(s1, rin), pol(s1 + lean + step * 0.05, rout), pol(s0 + lean + step * 0.35, rout)]
        d = "M" + " L".join(f"{x:.1f} {y:.1f}" for x, y in pts) + " Z"
        out.append((i, d, pts))
    return out


DIRECTIONS = {
    "a": {
        "name": "Warm Bronze · Midnight Slate",
        "bg": {
            "light": ("#273859", "#141D30", "#4A78C2"),
            "dark": ("#1C2436", "#0D111B", "#3B66B0"),
            "tinted": ("#2A303B", "#161920", "#8DA2C4"),
        },
        "bronze": ("#F5C78E", "#C98245", "#7B451F"),
        "bronze_shade": "#5C3114",
        "crest": ("#72A8F8", "#487EE0"),
        "ichor": ("#E3F0FF", "#5998F5", "#2B5FA6"),
        "glass_tint": 0.22,
    },
    "b": {
        "name": "Titanium Bronze · Silver Mist",
        "bg": {
            "light": ("#EEF1F6", "#D4DBE6", "#FFFFFF"),
            "dark": ("#242730", "#13151B", "#525B70"),
            "tinted": ("#D8DCE3", "#B9C0CC", "#FFFFFF"),
        },
        "bronze": ("#E8BD8A", "#B57846", "#6B4124"),
        "bronze_shade": "#4E2E18",
        "crest": ("#5B93E8", "#3568C4"),
        "ichor": ("#E8F3FF", "#4E8DF0", "#24549E"),
        "glass_tint": 0.34,
    },
    "c": {
        "name": "Architectural Relief · Obsidian Indigo",
        "bg": {
            "light": ("#1E2438", "#0F121D", "#608BE0"),
            "dark": ("#151722", "#090A0F", "#4A76CC"),
            "tinted": ("#222429", "#121317", "#9BA3B4"),
        },
        "bronze": ("#F0CCA0", "#BA7C49", "#693B1D"),
        "bronze_shade": "#4A2711",
        "crest": ("#85B6FF", "#4B80DC"),
        "ichor": ("#F0F7FF", "#68A5FF", "#2F66B8"),
        "glass_tint": 0.28,
    },
}


def render_full(dir_key, mode):
    cfg = DIRECTIONS[dir_key]
    bg0, bg1, rim = cfg["bg"][mode]
    b0, b1, b2 = cfg["bronze"]
    c0, c1 = cfg["crest"]
    i0, i1, i2 = cfg["ichor"]
    if mode == "tinted":
        b0, b1, b2 = "#E4E7EE", "#A5ACB8", "#5A606D"
        c0, c1 = "#D4DAE4", "#8B93A3"
        i0, i1, i2 = "#FFFFFF", "#B8C2D4", "#6B7485"
    blades = crest_blades()
    blade_svg = "\n  ".join(
        f'<path d="{d}" fill="url(#crestGrad)" opacity="{0.96 if i % 2 == 0 else 0.84:.2f}"/>'
        for i, d, _ in blades
    )
    return f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024">
<defs>
  <linearGradient id="bgGrad" x1="0.15" y1="0.05" x2="0.85" y2="0.95">
    <stop offset="0" stop-color="{bg0}"/>
    <stop offset="1" stop-color="{bg1}"/>
  </linearGradient>
  <radialGradient id="bgGlow" cx="0.62" cy="0.46" r="0.58">
    <stop offset="0" stop-color="{rim}" stop-opacity="0.26"/>
    <stop offset="1" stop-color="{rim}" stop-opacity="0"/>
  </radialGradient>
  <linearGradient id="bronzeGrad" x1="0.2" y1="0.1" x2="0.85" y2="0.95">
    <stop offset="0" stop-color="{b0}"/>
    <stop offset="0.52" stop-color="{b1}"/>
    <stop offset="1" stop-color="{b2}"/>
  </linearGradient>
  <linearGradient id="crestGrad" x1="0" y1="0" x2="0.8" y2="1">
    <stop offset="0" stop-color="{c0}"/>
    <stop offset="1" stop-color="{c1}"/>
  </linearGradient>
  <linearGradient id="ichorGrad" x1="0" y1="0" x2="0" y2="1">
    <stop offset="0" stop-color="{i0}"/>
    <stop offset="0.5" stop-color="{i1}"/>
    <stop offset="1" stop-color="{i2}"/>
  </linearGradient>
  <linearGradient id="glassSheen" x1="0.1" y1="0" x2="0.65" y2="0.85">
    <stop offset="0" stop-color="#FFFFFF" stop-opacity="{cfg['glass_tint'] + 0.16:.2f}"/>
    <stop offset="0.45" stop-color="#FFFFFF" stop-opacity="0.05"/>
    <stop offset="1" stop-color="#FFFFFF" stop-opacity="0"/>
  </linearGradient>
  <linearGradient id="rimStroke" x1="0" y1="0" x2="1" y2="1">
    <stop offset="0" stop-color="#FFFFFF" stop-opacity="0.55"/>
    <stop offset="0.5" stop-color="#FFFFFF" stop-opacity="0.10"/>
    <stop offset="1" stop-color="#FFFFFF" stop-opacity="0.28"/>
  </linearGradient>
  <filter id="softShadow" x="-15%" y="-15%" width="130%" height="130%">
    <feDropShadow dx="0" dy="18" stdDeviation="20" flood-color="#060912" flood-opacity="0.34"/>
  </filter>
  <clipPath id="squircle">
    <rect x="88" y="88" width="848" height="848" rx="196"/>
  </clipPath>
</defs>

<!-- Layer 1: Background Glass Tile -->
<rect x="88" y="88" width="848" height="848" rx="196" fill="url(#bgGrad)"/>
<g clip-path="url(#squircle)">
  <rect x="88" y="88" width="848" height="848" fill="url(#bgGlow)"/>

  <!-- Layer 2: Sculpted Talos Subject (no cartoon outlines) -->
  <g filter="url(#softShadow)">
    <!-- Swept-back crest blades -->
    {blade_svg}
    <!-- Torso -->
    <path d="{TORSO}" fill="url(#bronzeGrad)"/>
    <!-- Neck -->
    <path d="{NECK}" fill="url(#bronzeGrad)"/>
    <!-- Glowing blue ichor glass vein -->
    <path d="{VEIN}" fill="url(#ichorGrad)"/>
    <!-- Helmet main sculpted form -->
    <path d="{HELM}" fill="url(#bronzeGrad)"/>
    <!-- Subtle volumetric shadow on rear cheek -->
    <path d="{HELM_SHADE}" fill="{cfg['bronze_shade']}" opacity="0.28"/>
    <!-- Visor & eye slit -->
    <path d="{VISOR}" fill="#141926" opacity="0.90"/>
    <ellipse cx="644" cy="482" rx="16" ry="11" fill="url(#ichorGrad)"/>
  </g>

  <!-- Layer 3: Liquid Glass Specular Highlights -->
  <path d="M120 280 C240 130 540 100 840 150 L840 96 C540 84 220 96 100 220 Z" fill="url(#glassSheen)"/>
  <ellipse cx="420" cy="230" rx="280" ry="120" fill="url(#glassSheen)" transform="rotate(-14 420 230)"/>
  <path d="M510 302 C595 300 658 345 674 415" fill="none" stroke="#FFFFFF" stroke-opacity="0.38" stroke-width="10" stroke-linecap="round"/>
</g>
<rect x="89.5" y="89.5" width="845" height="845" rx="194.5" fill="none" stroke="url(#rimStroke)" stroke-width="3"/>
</svg>'''


def render_small(dir_key, size):
    cfg = DIRECTIONS[dir_key]
    bg0, bg1, _ = cfg["bg"]["light"]
    b0, b1, b2 = cfg["bronze"]
    c0, c1 = cfg["crest"]
    i0, i1, _ = cfg["ichor"]
    # Simplified silhouette for 16px and 32px: helmet + simplified crest + glowing eye
    return f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024">
<defs>
  <linearGradient id="sbg" x1="0.1" y1="0" x2="0.9" y2="1"><stop offset="0" stop-color="{bg0}"/><stop offset="1" stop-color="{bg1}"/></linearGradient>
  <linearGradient id="sbr" x1="0.2" y1="0.1" x2="0.8" y2="0.9"><stop offset="0" stop-color="{b0}"/><stop offset="1" stop-color="{b2}"/></linearGradient>
  <linearGradient id="scr" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="{c0}"/><stop offset="1" stop-color="{c1}"/></linearGradient>
</defs>
<rect x="64" y="64" width="896" height="896" rx="210" fill="url(#sbg)"/>
<path d="M620 310 C600 175 430 140 270 220 C220 290 215 390 250 470 C310 385 415 340 620 310 Z" fill="url(#scr)"/>
<path d="M520 270 C660 270 740 350 740 480 L740 760 L330 760 L330 480 C330 350 400 270 520 270 Z" fill="url(#sbr)"/>
<rect x="570" y="445" width="170" height="72" rx="28" fill="#141926"/>
<circle cx="645" cy="481" r="24" fill="{i1}"/>
</svg>'''


def main():
    for key in ("a", "b", "c"):
        for mode in ("light", "dark", "tinted"):
            path = os.path.join(OUT, f"talos-glass-{key}-{mode}.svg")
            with open(path, "w") as f:
                f.write(render_full(key, mode))
        for sz in (32, 16):
            path = os.path.join(OUT, f"talos-glass-{key}-{sz}.svg")
            with open(path, "w") as f:
                f.write(render_small(key, sz))
    print("generated 15 Liquid Glass draft SVGs in", OUT)


if __name__ == "__main__":
    main()
