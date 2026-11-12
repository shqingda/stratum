import math, random
random.seed(7)
HELM="M520 296 C620 292 690 350 698 432 L704 590 L700 700 C680 704 650 696 630 684 C600 664 580 642 566 640 C540 640 520 664 470 690 C440 704 400 712 366 704 C352 640 340 560 344 480 C350 370 420 300 520 296 Z"
CREST="M652 344 C656 214 566 140 452 150 C344 160 282 236 272 344 C266 410 280 470 304 520 C326 460 340 410 360 370 C400 330 450 320 520 322 C584 324 628 330 640 360 Z"
OPEN="M590 458 C604 446 660 444 720 452 L720 506 L700 508 L700 650 C694 660 674 660 668 650 L666 508 C640 510 606 508 594 498 C582 490 580 468 590 458 Z"
def strand(t, off):
    pts=[]
    for k in range(14):
        u=t+k*0.011
        a=math.radians(-10-200*u)
        r=170+off+(0 if u<.75 else (u-.75)*260)
        w=math.sin(u*40+off)*1.2
        pts.append((462+(r+w)*math.cos(a), 352+(r+w)*math.sin(a)*1.05+(0 if u<.8 else (u-.8)*300)))
    return 'M%.1f %.1f '%pts[0]+' '.join('L%.1f %.1f'%p for p in pts[1:])
hair=[]
for i in range(640):
    t=random.uniform(0,.86); off=random.uniform(-95,42)
    col=random.choice(['#9cc4f6','#6fa2ea','#4f86d8','#3a6fc0','#2b5fa6','#b9d6fb'])
    hair.append(f'<path d="{strand(t,off)}" stroke="{col}" stroke-width="{random.uniform(2,4.5):.1f}" fill="none" stroke-linecap="round" opacity="{random.uniform(.55,.95):.2f}"/>')
rivets=''.join(f'<circle cx="{x}" cy="{y}" r="9" fill="url(#rivet)"/>' for x,y in [(430,452),(478,438),(528,430)])
svg=f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024">
<defs>
 <linearGradient id="tile" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#ffffff"/><stop offset="1" stop-color="#e4e7ed"/></linearGradient>
 <radialGradient id="bronze" cx=".42" cy=".22" r=".95"><stop offset="0" stop-color="#ffd7a2"/><stop offset=".22" stop-color="#dc9a5a"/><stop offset=".55" stop-color="#a5622f"/><stop offset=".85" stop-color="#5e3118"/><stop offset="1" stop-color="#3f1f0e"/></radialGradient>
 <linearGradient id="nose" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="#8a4d25"/><stop offset=".45" stop-color="#f3c28a"/><stop offset="1" stop-color="#6b3a1b"/></linearGradient>
 <radialGradient id="inside" cx=".7" cy=".3" r=".9"><stop offset="0" stop-color="#1d2433"/><stop offset="1" stop-color="#07080c"/></radialGradient>
 <radialGradient id="crestbase" cx=".5" cy=".2" r=".9"><stop offset="0" stop-color="#7fb0f2"/><stop offset="1" stop-color="#1f4a8c"/></radialGradient>
 <radialGradient id="rivet" cx=".35" cy=".3" r=".75"><stop offset="0" stop-color="#fff1d6"/><stop offset=".5" stop-color="#d0904f"/><stop offset="1" stop-color="#5e3118"/></radialGradient>
 <filter id="hammer" x="0" y="0" width="100%" height="100%">
  <feTurbulence type="fractalNoise" baseFrequency=".022" numOctaves="3" seed="4" result="n"/>
  <feSpecularLighting in="n" surfaceScale="5" specularConstant=".9" specularExponent="22" lighting-color="#ffe6c4" result="s"><fePointLight x="380" y="160" z="420"/></feSpecularLighting>
  <feComposite in="s" in2="SourceAlpha" operator="in"/>
 </filter>
 <filter id="grain" x="0" y="0" width="100%" height="100%">
  <feTurbulence type="fractalNoise" baseFrequency="1.1" numOctaves="2" seed="9" result="g"/>
  <feColorMatrix in="g" type="matrix" values="0 0 0 0 .2  0 0 0 0 .1  0 0 0 0 .05  0 0 0 .9 -.35"/>
  <feComposite in2="SourceAlpha" operator="in"/>
 </filter>
 <filter id="b8" x="-.5" y="-.5" width="2" height="2"><feGaussianBlur stdDeviation="8"/></filter>
 <filter id="b20" x="-.5" y="-.5" width="2" height="2"><feGaussianBlur stdDeviation="22"/></filter>
 <clipPath id="c"><rect x="100" y="100" width="824" height="824" rx="186"/></clipPath>
 <path id="hp" d="{HELM}"/><clipPath id="helm"><use href="#hp"/></clipPath>
 <path id="cp" d="{CREST}"/><clipPath id="crest"><use href="#cp"/></clipPath>
</defs>
<rect x="100" y="100" width="824" height="824" rx="186" fill="url(#tile)"/>
<g clip-path="url(#c)">
 <g transform="translate(-6 44)">
 <ellipse cx="540" cy="726" rx="230" ry="30" fill="#1b2232" opacity=".38" filter="url(#b20)"/>
 <ellipse cx="530" cy="716" rx="170" ry="12" fill="#1b2232" opacity=".45" filter="url(#b8)"/>
 <use href="#cp" fill="url(#crestbase)"/>
 <g clip-path="url(#crest)">{''.join(hair)}
  <path d="M300 520 C300 380 360 290 450 270" stroke="#0f2340" stroke-width="60" fill="none" opacity=".35" filter="url(#b20)"/>
 </g>
 <use href="#hp" fill="url(#bronze)"/>
 <g clip-path="url(#helm)">
  <rect x="300" y="280" width="440" height="460" fill="#fff" filter="url(#hammer)" opacity=".35" style="mix-blend-mode:screen"/>
  <rect x="300" y="280" width="440" height="460" fill="#fff" filter="url(#grain)" opacity=".5"/>
  <path d="M320 300 C400 360 430 520 460 740 L300 740 Z" fill="#2a1408" opacity=".38" filter="url(#b20)"/>
  <path d="M370 432 C460 400 600 402 704 428" stroke="#5f9e98" stroke-width="10" fill="none" opacity=".35" filter="url(#b8)"/>
  <path d="M372 706 C430 714 480 696 530 666" stroke="#5f9e98" stroke-width="14" fill="none" opacity=".35" filter="url(#b8)"/>
  <path d="M370 424 C460 392 600 394 704 420" stroke="#4a250f" stroke-width="16" fill="none" opacity=".45" filter="url(#b8)" transform="translate(0 8)"/>
  <path d="M370 424 C460 392 600 394 704 420" stroke="#ffd9a8" stroke-width="5" fill="none" opacity=".8"/>
  <path d="M470 336 C540 314 620 328 664 372" stroke="#fff6e8" stroke-width="20" fill="none" stroke-linecap="round" opacity=".85" filter="url(#b8)"/>
  <path d="M496 334 C548 322 600 328 636 350" stroke="#fff" stroke-width="5" fill="none" stroke-linecap="round" opacity=".9"/>
  <path d="M562 520 C556 570 560 612 582 652" stroke="#4a250f" stroke-width="8" fill="none" opacity=".5"/>
  <path d="M574 520 C570 570 574 612 594 650" stroke="#ffd9a8" stroke-width="4" fill="none" opacity=".55"/>
  <path d="M372 700 C430 708 480 690 530 660 C556 646 580 640 600 654" stroke="#f7c58e" stroke-width="5" fill="none" opacity=".7"/>
 </g>
 <path d="{OPEN}" fill="url(#inside)"/>
 <ellipse cx="636" cy="482" rx="22" ry="10" fill="#7aa8ea" opacity=".55" filter="url(#b8)"/>
 <path d="{OPEN}" fill="none" stroke="#3a1c0b" stroke-width="5" opacity=".8"/>
 <path d="M688 440 L716 444 L718 596 C718 606 710 612 702 610 L694 606 Z" fill="url(#nose)"/>
 <path d="M698 452 L702 590" stroke="#fff3e0" stroke-width="3" opacity=".8"/>
 {rivets}
 </g>
</g>
<rect x="100.5" y="100.5" width="823" height="823" rx="185.5" fill="none" stroke="#12131A" stroke-opacity=".08"/>
</svg>'''
open('../talos-real.svg','w').write(svg)
