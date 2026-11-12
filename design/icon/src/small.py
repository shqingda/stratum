# Small-size Talos icon: the same bust, cropped to the head and stripped to
# what survives at 32 / 16 px (crest silhouette, two-tone helmet, glowing eye).
# usage: python3 small.py 32   -> ../talos-mech-32.svg
#        python3 small.py 16   -> ../talos-mech-16.svg
import math, sys
SIZE = int(sys.argv[1]) if len(sys.argv) > 1 else 32
T = SIZE == 16

INK = '#22150e'
SW = 22 if T else 16                       # outline, in artwork units (before zoom)
O = f'stroke="{INK}" stroke-width="{SW}" stroke-linejoin="round" stroke-linecap="round"'
B, BS, BH = '#d58c4b', '#a45f2e', '#f2bd80'
HELM = "M520 296 C620 292 690 350 698 432 L704 590 L700 700 C680 704 650 696 630 684 C600 664 580 642 566 640 C540 640 520 664 470 690 C440 704 400 712 366 704 C352 640 340 560 344 480 C350 370 420 300 520 296 Z"
OPEN = "M590 458 C604 446 660 444 720 452 L720 506 L700 508 L700 650 C694 660 674 660 668 650 L666 508 C640 510 606 508 594 498 C582 490 580 468 590 458 Z"

# swept crest, fewer and wider blades than the large icon
CX, CY = 522, 472
def pol(a, r): return CX + r*math.cos(math.radians(a)), CY + r*math.sin(math.radians(a))
N = 3 if T else 4
a0, a1 = -62, -150                 # root angles along the dome, front to back
W0, W1 = (104, 64) if T else (84, 52)  # width at the root and at the tip
step = (a1 - a0) / (N - 1)
blades = []
for i in range(N):
    th = a0 + i*step
    mid = i / (N - 1)
    L = (175 if T else 160) + 80*mid                        # longer toward the back
    rx, ry = pol(th, 140)                                   # root, hidden under the dome
    da = math.radians(-128 - 26*mid)                        # all lean back and up, nearly parallel
    dx, dy = math.cos(da), math.sin(da)
    nx, ny = -dy, dx
    tx, ty = rx + dx*(L + 60), ry + dy*(L + 60)
    p = [(rx + nx*W0/2, ry + ny*W0/2), (tx + nx*W1/2, ty + ny*W1/2),
         (tx - nx*W1/2 + dx*-22, ty - ny*W1/2 + dy*-22),      # tip cut on a slant
         (rx - nx*W0/2, ry - ny*W0/2)]
    d = 'M' + ' L'.join(f'{x:.1f} {y:.1f}' for x, y in p) + ' Z'
    # a background-coloured outline cuts each blade off the one behind it
    blades.insert(0, f'<path d="{d}" fill="#86b4ff" stroke="#1c2640" stroke-width="{24 if T else 18}" stroke-linejoin="round"/>')

# zoom on the head: artwork point (480, 500) lands at the tile centre
Z = 1.12 if T else 1.06
G = f'translate(512 {548 if T else 548}) scale({Z}) translate(-490 -520)'

rail = ''
if not T:
    ri, ro = 160, 194
    p1, p2, p3, p4 = pol(a0+2, ro), pol(a1-2, ro), pol(a1-2, ri), pol(a0+2, ri)
    rail = (f'<path d="M{p1[0]:.1f} {p1[1]:.1f} A{ro} {ro} 0 0 0 {p2[0]:.1f} {p2[1]:.1f} '
            f'L{p3[0]:.1f} {p3[1]:.1f} A{ri} {ri} 0 0 1 {p4[0]:.1f} {p4[1]:.1f} Z" fill="{BH}" '
            f'stroke="{INK}" stroke-width="{SW}" stroke-linejoin="round"/>')

vein = '' if T else (f'<path d="M598 640 L598 800" stroke="{INK}" stroke-width="48" stroke-linecap="round"/>'
                     f'<path d="M598 640 L598 800" stroke="#7aa8ea" stroke-width="26" stroke-linecap="round"/>')

svg = f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024">
<defs>
 <radialGradient id="bg" cx=".58" cy=".42" r=".8"><stop offset="0" stop-color="#2f4777"/><stop offset="1" stop-color="#141a2b"/></radialGradient>
 <clipPath id="c"><rect x="100" y="100" width="824" height="824" rx="186"/></clipPath>
 <path id="hp" d="{HELM}"/><clipPath id="helm"><use href="#hp"/></clipPath>
 <radialGradient id="glow"><stop offset="0" stop-color="#bcd6ff" stop-opacity=".55"/><stop offset="1" stop-color="#bcd6ff" stop-opacity="0"/></radialGradient>
</defs>
<rect x="100" y="100" width="824" height="824" rx="186" fill="url(#bg)"/>
<g clip-path="url(#c)"><g transform="{G}">
 <!-- shoulders and neck, silhouette only -->
 <path d="M150 1000 C170 860 250 800 360 786 C430 778 600 778 660 786 C770 804 840 860 870 1000 Z" fill="{B}" {O}/>
 <path d="M170 1000 C190 880 250 820 330 800 C316 870 318 940 332 1000 Z" fill="{BS}"/>
 <path d="M470 640 L470 790 L620 790 L620 640 Z" fill="{BS}" {O}/>
 {vein}
 {''.join(blades)}
 <use href="#hp" fill="{B}"/>
 <g clip-path="url(#helm)">
  <path d="M330 290 C420 340 450 520 480 740 L320 740 Z" fill="{BS}"/>
  <path d="M470 336 C560 314 640 336 676 388" stroke="{BH}" stroke-width="{34 if T else 28}" fill="none" stroke-linecap="round"/>
 </g>
 <use href="#hp" fill="none" {O}/>
 {rail}
 <path d="{OPEN}" fill="#150e15" {O}/>
 <circle cx="636" cy="480" r="{90 if T else 72}" fill="url(#glow)"/>
 <circle cx="636" cy="480" r="{30 if T else 26}" fill="#d6e7ff"/>
 <path d="M688 440 L716 444 L718 596 C718 606 710 612 702 610 L694 606 Z" fill="{BH}" {O}/>
</g></g>
</svg>'''
open(f'../talos-mech-{SIZE}.svg', 'w').write(svg)
