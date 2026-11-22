"""Generates light (default) and dark macOS App Icon SVGs for Stratum.

Produces:
  - design/icon/stratum-icon-{light,dark}.svg     (1024x1024 master)
  - design/icon/stratum-icon-{light,dark}-32.svg  (32x32 optical simplified)
  - design/icon/stratum-icon-{light,dark}-16.svg  (16x16 optical simplified)
"""
import os

OUT = os.path.abspath(os.path.join(os.path.dirname(__file__), ".."))

THEMES = {
    "light": {
        "bg": ("#FFFFFF", "#F6F7F9", "#EAEDF2"),
        "sheen": ("#FFFFFF", "0.95", "#DCE3F0", "0.08"),
        "rim": ("#000000", "0.07", "#000000", "0.10", "#000000", "0.12"),
        "inner_rim": ("#FFFFFF", "0.90", "#FFFFFF", "0.25", "#FFFFFF", "0.05"),
        "bar_top": ("#232730", "#14161C"),
        "bar_mid": ("#1C2028", "#101217"),
        "bar_bot": ("#2E333F", "#1B1E26"),
        "bar_shadow": ("#000000", "0.16"),
    },
    "dark": {
        "bg": ("#2C3240", "#171A22", "#101218"),
        "sheen": ("#FFFFFF", "0.14", "#8AA4D6", "0.04"),
        "rim": ("#FFFFFF", "0.24", "#FFFFFF", "0.08", "#FFFFFF", "0.03"),
        "inner_rim": None,
        "bar_top": ("#FFFFFF", "#E4E9F2"),
        "bar_mid": ("#FFFFFF", "#DCE6F8"),
        "bar_bot": ("#E8ECF4", "#C6CFE0"),
        "bar_shadow": ("#05070B", "0.42"),
    },
}


def squircle(x: float, y: float, size: float, r: float) -> str:
    """Returns an SVG path for a continuous macOS-style squircle."""
    c = r * 0.4477
    x0, x1 = x, x + size
    y0, y1 = y, y + size
    return (
        f"M {x0 + r:.2f} {y0:.2f} "
        f"L {x1 - r:.2f} {y0:.2f} "
        f"C {x1 - c:.2f} {y0:.2f} {x1:.2f} {y0 + c:.2f} {x1:.2f} {y0 + r:.2f} "
        f"L {x1:.2f} {y1 - r:.2f} "
        f"C {x1:.2f} {y1 - c:.2f} {x1 - c:.2f} {y1:.2f} {x1 - r:.2f} {y1:.2f} "
        f"L {x0 + r:.2f} {y1:.2f} "
        f"C {x0 + c:.2f} {y1:.2f} {x0:.2f} {y1 - c:.2f} {x0:.2f} {y1 - r:.2f} "
        f"L {x0:.2f} {y0 + r:.2f} "
        f"C {x0:.2f} {y0 + c:.2f} {x0 + c:.2f} {y0:.2f} {x0 + r:.2f} {y0:.2f} Z"
    )


def master_svg(theme_name: str) -> str:
    t = THEMES[theme_name]
    sq = squircle(100, 100, 824, 186)
    inner_sq = squircle(102, 102, 820, 184)
    # Bold, optically centered 3-bar mark matching the dark icon's proportions.
    # Mapped from 24x24 at scale=28, origin=(176, 176):
    #   x_span: 3.5..20.5 -> 274..750 (width 476, centered at 512)
    #   y_span: 4.5..19.5 -> 302..722 (height 420, centered at 512)
    bars = [
        ("barTop", 274, 302, 364, 90, 45),
        ("barMid", 344, 467, 406, 90, 45),
        ("barBot", 274, 632, 308, 90, 45),
    ]
    inner_rim_el = ""
    inner_rim_def = ""
    if t["inner_rim"] is not None:
        c0, o0, c1, o1, c2, o2 = t["inner_rim"]
        inner_rim_def = f'''
    <linearGradient id="innerRim" x1="512" y1="102" x2="512" y2="922" gradientUnits="userSpaceOnUse">
      <stop offset="0%" stop-color="{c0}" stop-opacity="{o0}"/>
      <stop offset="40%" stop-color="{c1}" stop-opacity="{o1}"/>
      <stop offset="100%" stop-color="{c2}" stop-opacity="{o2}"/>
    </linearGradient>'''
        inner_rim_el = f'\n    <path d="{inner_sq}" stroke="url(#innerRim)" stroke-width="2"/>'

    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="1024" height="1024" viewBox="0 0 1024 1024" fill="none">
  <defs>
    <linearGradient id="bgGrad" x1="180" y1="100" x2="844" y2="924" gradientUnits="userSpaceOnUse">
      <stop offset="0%" stop-color="{t["bg"][0]}"/>
      <stop offset="48%" stop-color="{t["bg"][1]}"/>
      <stop offset="100%" stop-color="{t["bg"][2]}"/>
    </linearGradient>
    <radialGradient id="topSheen" cx="320" cy="210" r="620" gradientUnits="userSpaceOnUse">
      <stop offset="0%" stop-color="{t["sheen"][0]}" stop-opacity="{t["sheen"][1]}"/>
      <stop offset="55%" stop-color="{t["sheen"][2]}" stop-opacity="{t["sheen"][3]}"/>
      <stop offset="100%" stop-color="#000000" stop-opacity="0"/>
    </radialGradient>
    <linearGradient id="rimGrad" x1="512" y1="100" x2="512" y2="924" gradientUnits="userSpaceOnUse">
      <stop offset="0%" stop-color="{t["rim"][0]}" stop-opacity="{t["rim"][1]}"/>
      <stop offset="35%" stop-color="{t["rim"][2]}" stop-opacity="{t["rim"][3]}"/>
      <stop offset="100%" stop-color="{t["rim"][4]}" stop-opacity="{t["rim"][5]}"/>
    </linearGradient>{inner_rim_def}
    <linearGradient id="barTop" x1="274" y1="302" x2="638" y2="392" gradientUnits="userSpaceOnUse">
      <stop offset="0%" stop-color="{t["bar_top"][0]}"/>
      <stop offset="100%" stop-color="{t["bar_top"][1]}"/>
    </linearGradient>
    <linearGradient id="barMid" x1="344" y1="467" x2="750" y2="557" gradientUnits="userSpaceOnUse">
      <stop offset="0%" stop-color="{t["bar_mid"][0]}"/>
      <stop offset="100%" stop-color="{t["bar_mid"][1]}"/>
    </linearGradient>
    <linearGradient id="barBot" x1="274" y1="632" x2="582" y2="722" gradientUnits="userSpaceOnUse">
      <stop offset="0%" stop-color="{t["bar_bot"][0]}"/>
      <stop offset="100%" stop-color="{t["bar_bot"][1]}"/>
    </linearGradient>
    <filter id="iconShadow" x="60" y="76" width="904" height="904" filterUnits="userSpaceOnUse" color-interpolation-filters="sRGB">
      <feDropShadow dx="0" dy="18" stdDeviation="20" flood-color="#000000" flood-opacity="0.28"/>
    </filter>
    <filter id="barShadow" x="230" y="270" width="564" height="500" filterUnits="userSpaceOnUse" color-interpolation-filters="sRGB">
      <feDropShadow dx="0" dy="6" stdDeviation="10" flood-color="{t["bar_shadow"][0]}" flood-opacity="{t["bar_shadow"][1]}"/>
    </filter>
    <clipPath id="squircleClip">
      <path d="{sq}"/>
    </clipPath>
  </defs>

  <g filter="url(#iconShadow)">
    <path d="{sq}" fill="url(#bgGrad)"/>
  </g>

  <g clip-path="url(#squircleClip)">
    <rect x="100" y="100" width="824" height="824" fill="url(#topSheen)"/>
    <g filter="url(#barShadow)">
      <rect x="{bars[0][1]}" y="{bars[0][2]}" width="{bars[0][3]}" height="{bars[0][4]}" rx="{bars[0][5]}" fill="url(#barTop)"/>
      <rect x="{bars[1][1]}" y="{bars[1][2]}" width="{bars[1][3]}" height="{bars[1][4]}" rx="{bars[1][5]}" fill="url(#barMid)"/>
      <rect x="{bars[2][1]}" y="{bars[2][2]}" width="{bars[2][3]}" height="{bars[2][4]}" rx="{bars[2][5]}" fill="url(#barBot)"/>
    </g>{inner_rim_el}
  </g>

  <path d="{sq}" stroke="url(#rimGrad)" stroke-width="2.5"/>
</svg>
'''


def small_svg(theme_name: str, size: int) -> str:
    t = THEMES[theme_name]
    pad = 2 if size == 32 else 1
    inner = size - 2 * pad
    r = inner * 0.225
    sq = squircle(pad, pad, inner, r)
    bar_col = t["bar_mid"][0]
    bot_col = t["bar_bot"][0]
    stroke_attr = ' stroke="#000000" stroke-opacity="0.10" stroke-width="0.6"' if theme_name == "light" else ""
    if size == 32:
        bars = [
            (7.5, 9.0, 12.0, 3.2, 1.6, bar_col),
            (10.0, 14.4, 14.5, 3.2, 1.6, bar_col),
            (7.5, 19.8, 10.5, 3.2, 1.6, bot_col),
        ]
    else:
        bars = [
            (3.5, 4.2, 6.5, 1.9, 0.95, bar_col),
            (4.8, 7.05, 7.7, 1.9, 0.95, bar_col),
            (3.5, 9.9, 5.5, 1.9, 0.95, bot_col),
        ]
    rects = "\n  ".join(
        f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{rx}" fill="{col}"/>'
        for x, y, w, h, rx, col in bars
    )
    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="{size}" height="{size}" viewBox="0 0 {size} {size}" fill="none">
  <defs>
    <linearGradient id="bgSmall" x1="0" y1="0" x2="{size}" y2="{size}" gradientUnits="userSpaceOnUse">
      <stop offset="0%" stop-color="{t["bg"][0]}"/>
      <stop offset="100%" stop-color="{t["bg"][2]}"/>
    </linearGradient>
  </defs>
  <path d="{sq}" fill="url(#bgSmall)"{stroke_attr}/>
  {rects}
</svg>
'''


def main() -> None:
    for theme in ("light", "dark"):
        files = {
            f"stratum-icon-{theme}.svg": master_svg(theme),
            f"stratum-icon-{theme}-32.svg": small_svg(theme, 32),
            f"stratum-icon-{theme}-16.svg": small_svg(theme, 16),
        }
        for name, body in files.items():
            path = os.path.join(OUT, name)
            with open(path, "w", encoding="utf-8") as f:
                f.write(body)
            print("wrote", path)


if __name__ == "__main__":
    main()
