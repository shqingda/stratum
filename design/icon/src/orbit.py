import math
cx=cy=512; R=272
dot=math.radians(-48); span=math.radians(290)
pts_o=[];pts_i=[];N=160
for i in range(N+1):
    t=i/N; a=dot-span*(1-t); w=4+30*(t**1.3)
    pts_o.append((cx+(R+w/2)*math.cos(a),cy+(R+w/2)*math.sin(a))); pts_i.append((cx+(R-w/2)*math.cos(a),cy+(R-w/2)*math.sin(a)))
poly=pts_o+pts_i[::-1]; B=R+40; ox,oy=cx-B,cy-B
clip='M'+' L'.join(f'{x-ox:.1f} {y-oy:.1f}' for x,y in poly)+' Z'
start=math.degrees(dot)+90-math.degrees(span); sp=math.degrees(span)
px,py=cx+R*math.cos(dot),cy+R*math.sin(dot)
svg=f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024">
<defs>
 <linearGradient id="tile" x1=".2" y1="0" x2=".5" y2="1"><stop offset="0" stop-color="#232734"/><stop offset=".55" stop-color="#14161e"/><stop offset="1" stop-color="#0b0c11"/></linearGradient>
 <radialGradient id="ambient" cx="{px/1024:.3f}" cy="{py/1024:.3f}" r=".75"><stop offset="0" stop-color="#3d6fb8" stop-opacity=".42"/><stop offset=".45" stop-color="#2B5FA6" stop-opacity=".10"/><stop offset="1" stop-color="#2B5FA6" stop-opacity="0"/></radialGradient>
 <radialGradient id="disc" cx=".62" cy=".3" r=".9"><stop offset="0" stop-color="#232a3b"/><stop offset=".6" stop-color="#141720"/><stop offset="1" stop-color="#0e1016"/></radialGradient>
 <linearGradient id="rim" x1="1" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#9cc0f2" stop-opacity=".55"/><stop offset=".45" stop-color="#9cc0f2" stop-opacity=".04"/><stop offset="1" stop-color="#fff" stop-opacity=".06"/></linearGradient>
 <linearGradient id="inner" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#000" stop-opacity=".55"/><stop offset=".35" stop-color="#000" stop-opacity="0"/></linearGradient>
 <radialGradient id="dot" cx=".38" cy=".34" r=".7"><stop offset="0" stop-color="#fff"/><stop offset=".5" stop-color="#e2edfd"/><stop offset="1" stop-color="#8fb6ee"/></radialGradient>
 <radialGradient id="bloom" cx=".5" cy=".5" r=".5"><stop offset="0" stop-color="#a9c9f6" stop-opacity=".75"/><stop offset=".35" stop-color="#7AA8EA" stop-opacity=".28"/><stop offset="1" stop-color="#7AA8EA" stop-opacity="0"/></radialGradient>
 <radialGradient id="cast" cx=".5" cy=".5" r=".5"><stop offset="0" stop-color="#7AA8EA" stop-opacity=".22"/><stop offset="1" stop-color="#7AA8EA" stop-opacity="0"/></radialGradient>
 <linearGradient id="sheen" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#fff" stop-opacity=".06"/><stop offset=".4" stop-color="#fff" stop-opacity="0"/></linearGradient>
 <linearGradient id="edge" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#fff" stop-opacity=".18"/><stop offset=".25" stop-color="#fff" stop-opacity=".04"/><stop offset="1" stop-color="#fff" stop-opacity=".02"/></linearGradient>
 <clipPath id="c"><rect x="100" y="100" width="824" height="824" rx="186"/></clipPath>
 <clipPath id="dc"><circle cx="512" cy="512" r="188"/></clipPath>
</defs>
<rect x="100" y="100" width="824" height="824" rx="186" fill="url(#tile)"/>
<g clip-path="url(#c)">
 <rect x="100" y="100" width="824" height="824" fill="url(#ambient)"/>
 <circle cx="512" cy="512" r="188" fill="url(#disc)"/>
 <g clip-path="url(#dc)"><circle cx="512" cy="512" r="188" fill="url(#inner)"/><circle cx="{px-70:.1f}" cy="{py+70:.1f}" r="170" fill="url(#cast)"/></g>
 <circle cx="512" cy="512" r="187" fill="none" stroke="url(#rim)" stroke-width="2.5"/>
 <circle cx="512" cy="512" r="{R}" fill="none" stroke="#fff" stroke-opacity=".06" stroke-width="2"/>
 <foreignObject x="{ox:.1f}" y="{oy:.1f}" width="{2*B}" height="{2*B}"><div xmlns="http://www.w3.org/1999/xhtml" style="width:{2*B}px;height:{2*B}px;clip-path:path('{clip}');background:conic-gradient(from {start-8:.2f}deg at 50% 50%, rgba(43,95,166,0) 0deg, rgba(43,95,166,0) 8deg, rgba(60,110,185,.55) {8+sp*.55:.1f}deg, #b9d3f8 {8+sp:.1f}deg, #b9d3f8 {12+sp:.1f}deg, rgba(43,95,166,0) {13+sp:.1f}deg)"></div></foreignObject>
 <circle cx="{px:.1f}" cy="{py:.1f}" r="130" fill="url(#bloom)"/>
 <circle cx="{px:.1f}" cy="{py:.1f}" r="34" fill="url(#dot)"/>
 <circle cx="{px-10:.1f}" cy="{py-11:.1f}" r="9" fill="#fff" opacity=".9"/>
 <rect x="100" y="100" width="824" height="420" fill="url(#sheen)"/>
</g>
<rect x="101" y="101" width="822" height="822" rx="185" fill="none" stroke="url(#edge)" stroke-width="2"/>
</svg>'''
open('../talos-orbit.svg','w').write(svg)
