import math, sys
UPRIGHT = len(sys.argv)>1 and sys.argv[1]=='upright'   # archived: upright comb crest
O='stroke="#22150e" stroke-width="12" stroke-linejoin="round" stroke-linecap="round"'
O8='stroke="#22150e" stroke-width="8" stroke-linejoin="round" stroke-linecap="round"'
B,BS,BH,BR,BD='#d58c4b','#a45f2e','#f2bd80','#ffe4bf','#6e3c1c'
HELM="M520 296 C620 292 690 350 698 432 L704 590 L700 700 C680 704 650 696 630 684 C600 664 580 642 566 640 C540 640 520 664 470 690 C440 704 400 712 366 704 C352 640 340 560 344 480 C350 370 420 300 520 296 Z"
CREST="M652 344 C656 214 566 140 452 150 C344 160 282 236 272 344 C266 410 280 470 304 520 C326 460 340 410 360 370 C400 330 450 320 520 322 C584 324 628 330 640 360 Z"
OPEN="M590 458 C604 446 660 444 720 452 L720 506 L700 508 L700 650 C694 660 674 660 668 650 L666 508 C640 510 606 508 594 498 C582 490 580 468 590 458 Z"
rays=''.join(f'<path d="M580 480 L{580+900*math.cos(a/12*math.pi):.0f} {480+900*math.sin(a/12*math.pi):.0f} L{580+900*math.cos((a+.55)/12*math.pi):.0f} {480+900*math.sin((a+.55)/12*math.pi):.0f} Z"/>' for a in range(0,24,2))
def rivet(x,y,r=9): return f'<circle cx="{x}" cy="{y}" r="{r}" fill="{BR}" stroke="#22150e" stroke-width="4.5"/>'
def gear(cx,cy,r,teeth=10,fill=BH):
    pts=[]
    for i in range(teeth*2):
        a=i*math.pi/teeth; rr=r if i%2==0 else r*.78
        a1=a-math.pi/teeth*.42; a2=a+math.pi/teeth*.42
        pts+= [(cx+rr*math.cos(a1),cy+rr*math.sin(a1)),(cx+rr*math.cos(a2),cy+rr*math.sin(a2))]
    d='M'+' L'.join(f'{x:.1f} {y:.1f}' for x,y in pts)+' Z'
    return f'<path d="{d}" fill="{fill}" {O8}/><circle cx="{cx}" cy="{cy}" r="{r*.32:.1f}" fill="{BD}" {O8}/>'

CX,CY=522,472
def pol(a,r): return CX+r*math.cos(math.radians(a)), CY+r*math.sin(math.radians(a))
blades=[];N=9;a0=-38;a1=-148;gap=3.2
step=(a1-a0)/N
for i in range(N):
    s0=a0+i*step+ (gap/2 if step>0 else -gap/2); s1=a0+(i+1)*step-(gap/2 if step>0 else -gap/2)
    mid=(i+.5)/N
    if not UPRIGHT:
        # swept back: blades lean toward the back, grow taller toward the rear, and taper
        h=70+120*math.sin(min(1,mid*1.25)*math.pi*.5)**1.4*(1-.25*max(0,mid-.8)/.2)
        lean=step*1.7                          # tips shifted toward the back
        rin=186; rout=rin+h
        p=[pol(s0,rin),pol(s1,rin),pol(s1+lean+step*.05,rout),pol(s0+lean+step*.35,rout)]
    else:
        h=78+86*math.sin(mid*math.pi)            # tallest in the middle
        rin=186; rout=rin+h
        p=[pol(s0,rin),pol(s1,rin),pol(s1+(step*.06),rout),pol(s0-(step*.06),rout)]
    d='M'+' L'.join(f'{x:.1f} {y:.1f}' for x,y in p)+' Z'
    col='#6a9ef0' if i%2==0 else '#5389dc'
    hl=[p[0],p[3]]; hl=[(hl[0][0]*.92+hl[1][0]*.08,hl[0][1]*.92+hl[1][1]*.08),(hl[0][0]*.1+hl[1][0]*.9,hl[0][1]*.1+hl[1][1]*.9)]
    blades.append(f'<path d="{d}" fill="{col}" {O8}/><path d="M{hl[0][0]:.1f} {hl[0][1]:.1f} L{hl[1][0]:.1f} {hl[1][1]:.1f}" stroke="#c3dafc" stroke-width="7" stroke-linecap="round" opacity=".8"/>')
# rail: an arc band riding on the dome
ri,ro=166,196
ra,rb=a0+2,a1-2
p1=pol(ra,ro);p2=pol(rb,ro);p3=pol(rb,ri);p4=pol(ra,ri)
RAIL=f'M{p1[0]:.1f} {p1[1]:.1f} A{ro} {ro} 0 0 0 {p2[0]:.1f} {p2[1]:.1f} L{p3[0]:.1f} {p3[1]:.1f} A{ri} {ri} 0 0 1 {p4[0]:.1f} {p4[1]:.1f} Z'
railrivets=''.join(f'<circle cx="{pol(a,181)[0]:.1f}" cy="{pol(a,181)[1]:.1f}" r="7" fill="#ffe4bf" stroke="#22150e" stroke-width="4.5"/>' for a in [a0+step*(i+.5) for i in range(N)])

# (old hair fins removed)
if False:

    fins=[]
    for i in range(9):
        t0=i/9; t1=(i+1)/9
        def P(t,r):
            a=math.radians(-12-196*t); rr=r+(0 if t<.72 else (t-.72)*240)
            return 462+rr*math.cos(a), 352+rr*math.sin(a)*1.05+(0 if t<.8 else (t-.8)*300)
        a=P(t0,212);b=P(t1,212);c=P(t1,120);d=P(t0,120)
        col='#6c9ff0' if i%2==0 else '#5288dd'
        fins.append(f'<path d="M{a[0]:.1f} {a[1]:.1f} L{b[0]:.1f} {b[1]:.1f} L{c[0]:.1f} {c[1]:.1f} L{d[0]:.1f} {d[1]:.1f} Z" fill="{col}"/>')
svg=f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024">
<defs>
 <radialGradient id="bg" cx=".58" cy=".45" r=".75"><stop offset="0" stop-color="#2f4777"/><stop offset="1" stop-color="#141a2b"/></radialGradient>
 <clipPath id="c"><rect x="100" y="100" width="824" height="824" rx="186"/></clipPath>
 <path id="hp" d="{HELM}"/><clipPath id="helm"><use href="#hp"/></clipPath>
 <path id="cp" d="{CREST}"/><clipPath id="crest"><use href="#cp"/></clipPath>
 <path id="bp" d="M140 924 C160 830 240 770 350 752 C420 742 600 742 660 752 C770 772 850 836 880 924 Z"/><clipPath id="body"><use href="#bp"/></clipPath>
 <radialGradient id="ichor" cx=".5" cy=".5" r=".5"><stop offset="0" stop-color="#d6e7ff"/><stop offset="1" stop-color="#5d93e6"/></radialGradient>
</defs>
<rect x="100" y="100" width="824" height="824" rx="186" fill="url(#bg)"/>
<g clip-path="url(#c)">
 <g opacity=".06" fill="#fff">{rays}</g>

 <!-- torso: plated cuirass with a porthole showing the gear inside -->
 <use href="#bp" fill="{B}"/>
 <g clip-path="url(#body)">
  <path d="M140 924 C170 840 230 790 320 764 C300 830 304 880 330 924 Z" fill="{BS}"/>
  <path d="M660 770 C750 790 816 834 848 890" stroke="{BH}" stroke-width="18" fill="none" stroke-linecap="round"/>
  <path d="M330 764 C320 830 322 880 340 924" stroke="#22150e" stroke-width="7" fill="none"/>
  <path d="M760 790 C740 840 736 890 744 924" stroke="#22150e" stroke-width="7" fill="none"/>
  {''.join(rivet(x,y,8) for x,y in [(352,800),(350,850),(352,900),(738,812),(732,860),(732,906)])}
  <!-- porthole -->
  <circle cx="548" cy="850" r="74" fill="{BS}" {O}/>
  <circle cx="548" cy="850" r="54" fill="#1a2134" {O8}/>
  <g transform="rotate(12 548 850)">{gear(548,850,44,9,'#c48447')}</g>
  <path d="M512 816 C526 804 546 800 562 804" stroke="#fff" stroke-opacity=".5" stroke-width="7" fill="none" stroke-linecap="round"/>
  {''.join(rivet(548+66*math.cos(a), 850+66*math.sin(a),6) for a in [i*math.pi/4 for i in range(8)])}
 </g>
 <use href="#bp" fill="none" {O}/>

 <!-- neck: stacked rings + the single vein as a glass tube of glowing ichor -->
 <path d="M462 640 L462 760 L612 760 L612 640 Z" fill="{BD}" {O}/>
 <g {O8}><rect x="448" y="664" width="178" height="26" rx="10" fill="{BS}"/><rect x="448" y="700" width="178" height="26" rx="10" fill="{BS}"/><rect x="440" y="736" width="194" height="26" rx="10" fill="{BS}"/></g>
 <path d="M592 640 L592 780" stroke="#22150e" stroke-width="30" stroke-linecap="round"/>
 <path d="M592 640 L592 780" stroke="#5d93e6" stroke-width="16" stroke-linecap="round"/>
 <path d="M592 646 L592 774" stroke="#d6e7ff" stroke-width="5" stroke-linecap="round"/>

 <!-- crest: a comb of blades -->
 {''.join(blades)}
 <!-- helmet with panel seams, rivet rows, a hinge bolt -->
 <use href="#hp" fill="{B}"/>
 <g clip-path="url(#helm)">
  <path d="M330 290 C420 340 450 520 480 740 L320 740 Z" fill="{BS}"/>
  <path d="M380 420 C460 390 600 392 700 418" stroke="#22150e" stroke-width="7" fill="none"/>
  <path d="M384 440 C460 412 600 414 700 438" stroke="{BS}" stroke-width="7" fill="none"/>
  <path d="M560 520 C556 570 560 610 580 650" stroke="#22150e" stroke-width="7" fill="none"/>
  <path d="M520 300 C500 340 494 380 498 410" stroke="#22150e" stroke-width="6" fill="none"/>
  <path d="M480 330 C560 312 640 334 676 384" stroke="{BR}" stroke-width="22" fill="none" stroke-linecap="round"/>
  <path d="M604 560 C620 600 640 630 660 650" stroke="{BH}" stroke-width="14" fill="none" stroke-linecap="round"/>
 </g>
 <use href="#hp" fill="none" {O}/>
 {''.join(rivet(x,y) for x,y in [(404,446),(446,432),(490,424),(534,420),(578,420),(622,424)])}
 {''.join(rivet(x,y,7) for x,y in [(574,560),(580,600),(592,636)])}
 <!-- hinge bolt where the cheek piece pivots -->
 <circle cx="500" cy="540" r="30" fill="{BH}" {O8}/><circle cx="500" cy="540" r="14" fill="{BS}" stroke="#22150e" stroke-width="5"/><path d="M490 530 L510 550" stroke="#22150e" stroke-width="5" stroke-linecap="round"/>

 <!-- crest rail sits on the dome -->
 <path d="{RAIL}" fill="#d58c4b" {O8}/>
 <path d="{RAIL}" fill="none" stroke="#f2bd80" stroke-width="0"/>
 {railrivets}
 <!-- opening, lens eye -->
 <path d="{OPEN}" fill="#150e15" {O}/>
 <circle cx="634" cy="480" r="30" fill="#9cc2ff" opacity=".18"/>
 <circle cx="634" cy="480" r="20" fill="#24324f" stroke="#7aa8ea" stroke-width="5"/>
 <circle cx="634" cy="480" r="9" fill="#d6e7ff"/>
 <circle cx="628" cy="474" r="3.5" fill="#fff"/>
 <path d="M688 440 L716 444 L718 596 C718 606 710 612 702 610 L694 606 Z" fill="{BH}" {O}/>
 {rivet(703,470,6)}{rivet(705,570,6)}
</g>
<rect x="100.5" y="100.5" width="823" height="823" rx="185.5" fill="none" stroke="#fff" stroke-opacity=".1"/>
</svg>'''
open('../talos-mech-upright.svg' if UPRIGHT else '../talos-mech.svg','w').write(svg)
