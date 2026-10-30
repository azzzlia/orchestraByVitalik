"""Generates ui/pet_animal_frames.go: a puppy and a kitten in 12 moods, saddest first.

Same moods and animations as the tyan (contrib/pet/tyan.py): a rain cloud and tears at
0 hours, zzz when bored, sparkles, star eyes and hearts as the density grows.

Run from the repo root: python3 contrib/pet/animals.py [preview.png]
"""
from PIL import Image, ImageDraw
import math, os, sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from tyan import braille  # noqa: E402

W, H = 52, 64   # px -> 26 cols x 16 rows
S = 6
MOODS = 12


def draw(kind, mood, blink=False, dy=0, phase=0):
    im = Image.new('L', (W*S, H*S), 0); d = ImageDraw.Draw(im)
    lw = int(S*1.1)

    def P(x, y, o=None):
        return (x*S, (y+(dy if o is None else o))*S)

    def L(pts, o=None, w=lw):
        d.line([P(x, y, o) for x, y in pts], fill=255, width=w, joint='curve')

    def A(box, a0, a1, o=None):
        x0, y0 = P(box[0], box[1], o); x1, y1 = P(box[2], box[3], o)
        d.arc([x0, y0, x1, y1], a0, a1, fill=255, width=lw)

    def E(box, fill=False, o=None):
        x0, y0 = P(box[0], box[1], o); x1, y1 = P(box[2], box[3], o)
        d.ellipse([x0, y0, x1, y1], fill=255) if fill else d.ellipse([x0, y0, x1, y1], outline=255, width=lw)

    def hole(box):
        x0, y0 = P(box[0], box[1]); x1, y1 = P(box[2], box[3])
        d.ellipse([x0, y0, x1, y1], fill=0)

    def poly(pts, o=None):
        d.polygon([P(x, y, o) for x, y in pts], fill=255)

    def sparkle(cx, cy, r=2):
        L([(cx-r, cy), (cx+r, cy)], 0); L([(cx, cy-r), (cx, cy+r)], 0)

    def heart(cx, cy, r=2.2, o=0):
        pts = []
        for i in range(24):
            t = 2*math.pi*i/24
            x = 16*math.sin(t)**3
            y = 13*math.cos(t)-5*math.cos(2*t)-2*math.cos(3*t)-math.cos(4*t)
            pts.append((cx+x*r/16, cy-y*r/16))
        poly(pts, o)

    def star(cx, cy, r=4):
        pts = []
        for i in range(10):
            a = -math.pi/2 + i*math.pi/5
            rr = r if i % 2 == 0 else r*0.45
            pts.append((cx+rr*math.cos(a), cy+rr*math.sin(a)))
        poly(pts)

    # --- the animal (same for every mood) ---
    if kind == 'dog':
        ey, ex = 26, (19, 33)
        E((11, 12, 41, 43))                                   # head
        E((22.5, 30, 29.5, 34.5), fill=True)                  # nose
        L([(26, 34.5), (26, 36)])
        # floppy ears hanging over the sides of the head
        poly([(15, 13), (9, 14), (4, 22), (3, 32), (6, 37), (10, 34), (12, 24)])
        poly([(37, 13), (43, 14), (48, 22), (49, 32), (46, 37), (42, 34), (40, 24)])
        # collar with a tag, shoulders, paws
        L([(15, 42), (21, 45), (26, 46), (31, 45), (37, 42)])
        E((24, 46, 28, 50))
        L([(14, 43), (11, 52), (10, 62)]); L([(38, 43), (41, 52), (42, 62)])
        A((14, 58, 22, 66), 180, 360); A((30, 58, 38, 66), 180, 360)
        mx, my = 26, 37
    else:
        ey, ex = 27, (19, 33)
        E((10, 15, 42, 44))                                   # head
        poly([(12, 22), (12, 6), (23, 16)]); poly([(40, 22), (40, 6), (29, 16)])  # ears
        x0, y0 = P(15, 12); x1, y1 = P(19, 17)
        d.polygon([P(14.5, 18), P(14.5, 11), P(19, 16)], fill=0)
        d.polygon([P(37.5, 18), P(37.5, 11), P(33, 16)], fill=0)
        poly([(24.5, 32), (27.5, 32), (26, 34)])             # nose
        for y0, y1 in ((32, 30), (34.5, 34.5), (37, 39)):     # whiskers
            L([(15, y0), (3, y1)], w=int(S*0.8)); L([(37, y0), (49, y1)], w=int(S*0.8))
        L([(15, 43), (12, 52), (12, 62)]); L([(37, 43), (40, 52), (40, 62)])
        A((15, 58, 23, 66), 180, 360); A((29, 58, 37, 66), 180, 360)
        L([(40, 60), (46, 58), (49, 52), (48, 46), (45, 44)])  # tail
        mx, my = 26, 35

    def open_eyes(big=False):
        for cx in ex:
            E((cx-3.5, ey-4, cx+3.5, ey+4), fill=True)
            hole((cx-1.8, ey-2.8, cx+0.2, ey-0.8))
            if big:
                hole((cx+0.6, ey+0.8, cx+1.8, ey+2))

    def closed():
        for cx in ex: A((cx-4, ey-2, cx+4, ey+4), 20, 160)

    def happy_eyes():
        for cx in ex: A((cx-4, ey-1, cx+4, ey+5), 200, 340)

    def half_eyes():
        for cx in ex:
            L([(cx-4, ey-1), (cx+4, ey-1)])
            x0, y0 = P(cx-3.5, ey-1); x1, y1 = P(cx+3.5, ey+4)
            d.chord([x0, y0, x1, y1], 0, 180, fill=255)

    def flat_eyes():
        for cx in ex: L([(cx-3.5, ey+1), (cx+3.5, ey+1)])

    def cry_eyes():
        L([(ex[0]-4, ey-3), (ex[0]+3, ey), (ex[0]-4, ey+3)]); L([(ex[1]+4, ey-3), (ex[1]-3, ey), (ex[1]+4, ey+3)])

    def brows_sad():
        L([(ex[0]-4, ey-6), (ex[0]+3, ey-8)]); L([(ex[1]-3, ey-8), (ex[1]+4, ey-6)])

    def blush():
        for cx in (13.5, 38.5): L([(cx-2, ey+6), (cx, ey+5)]); L([(cx+1, ey+6), (cx+3, ey+5)])

    def w_mouth(depth=2.0, wide=3.5):   # the cat's ω / the dog's smile under the nose
        A((mx-wide, my-depth, mx, my+depth), 0, 180); A((mx, my-depth, mx+wide, my+depth), 0, 180)

    # eyes
    if blink and mood in (2, 3, 5, 6, 7): closed()
    elif mood == 0: cry_eyes(); brows_sad()
    elif mood == 1: open_eyes(); brows_sad()
    elif mood == 2: half_eyes(); brows_sad()
    elif mood == 3: half_eyes()
    elif mood == 4: flat_eyes()
    elif mood in (5, 6): open_eyes()
    elif mood == 7: open_eyes(True)
    elif mood in (8, 9): happy_eyes()
    elif mood == 10:
        for cx in ex: star(cx, ey+0.5)
    else:
        for cx in ex: heart(cx, ey+0.5, 4.4, dy)

    # mouth
    if mood == 0: E((mx-3, my, mx+3, my+6))                           # wailing
    elif mood in (1, 2): A((mx-3, my+1, mx+3, my+5), 200, 340)        # frown
    elif mood == 3: L([(mx-3, my+2), (mx-1, my+1), (mx+1, my+3), (mx+3, my+2)])
    elif mood == 4: L([(mx-2, my+2), (mx+2, my+2)])
    elif mood == 5: w_mouth(1.5, 3)
    elif mood == 6: w_mouth(2.2, 3.5)
    elif mood == 7: w_mouth(2.8, 4)
    else:                                                              # open, happy
        x0, y0 = P(mx-4, my-2); x1, y1 = P(mx+4, my+5)
        d.chord([x0, y0, x1, y1], 0, 180, outline=255, width=lw)
        if kind == 'dog' and mood >= 9:                                # tongue out
            E((mx-2, my+3, mx+2, my+8), fill=True)

    # extras and animation (same as the tyan)
    if mood >= 7: blush()
    if mood == 0:
        E((3, 1, 11, 7), o=0); E((8, -1, 16, 6), o=0); E((12, 2, 19, 7), o=0)
        for x in (7, 12, 16): L([(x, 9+phase), (x, 11+phase)], 0)
        for x in (ex[0]-3, ex[1]+3): L([(x, ey+3+phase), (x, ey+8+phase)])
    if mood == 1: L([(ex[0], ey+5+phase), (ex[0], ey+8+phase)])
    if mood == 2: L([(ex[1], ey+5+phase), (ex[1], ey+7+phase)])
    if mood == 3: L([(44, 14), (45, 18)])
    if mood == 4: L([(43, 5-phase), (46, 5-phase), (43, 8-phase), (46, 8-phase)], 0)
    if mood == 9:
        for i, (cx, cy) in enumerate(((4, 8), (48, 6), (3, 44), (49, 40))):
            if (i + phase) % 2 == 0: sparkle(cx, cy)
    if mood == 10:
        for i, (cx, cy) in enumerate(((4, 8), (48, 6))):
            sparkle(cx, cy, 2 + (i + phase) % 2)
        heart(4, 44 - phase, 1.8); heart(48, 42 + phase - 2, 1.8)
    if mood == 11:
        for i, (cx, cy) in enumerate(((4, 12), (48, 10), (8, 3), (44, 2))):
            heart(cx, cy - ((phase + i) % 3) * 2, 2.2)
    return im.resize((W, H), Image.LANCZOS).point(lambda v: 255 if v > 90 else 0)


def frames(kind, mood):
    b = lambda **kw: braille(draw(kind, mood, **kw))
    if mood in (2, 3, 5, 6, 7):
        a, c = b(), b(blink=True)
        if mood == 2:
            return [b(phase=p) for p in (0, 0, 2, 2)] + [c]
        return [a, a, a, a, a, c]
    if mood in (0, 1):
        return [b(phase=p) for p in (0, 2, 4)]
    if mood == 4:
        return [b(phase=p) for p in (0, 1, 2, 3)]
    if mood == 8:
        return [b(dy=1), b(dy=-1)]
    if mood in (9, 10):
        return [b(dy=1, phase=0), b(dy=-1, phase=1)]
    return [b(dy=(1 if p % 2 == 0 else -1), phase=p) for p in range(3)]


def go_var(name, allf):
    out = ['var %s = [][][]string{' % name]
    for mf in allf:
        out.append('\t{')
        for fr in mf:
            out.append('\t\t{')
            out += ['\t\t\t"%s",' % r.rstrip('⠀') for r in fr]
            out.append('\t\t},')
        out.append('\t},')
    out.append('}')
    return out


if __name__ == '__main__':
    out = ['// Code generated by contrib/pet/animals.py; DO NOT EDIT.', '', 'package ui', '',
           '// dogFrames and catFrames [mood][frame] are the braille puppy and kitten, saddest mood first.']
    out += go_var('dogFrames', [frames('dog', m) for m in range(MOODS)])
    out += [''] + go_var('catFrames', [frames('cat', m) for m in range(MOODS)])
    open('ui/pet_animal_frames.go', 'w').write('\n'.join(out) + '\n')
    if len(sys.argv) > 1:
        cols, C = 6, 6
        sheet = Image.new('RGB', (cols*(W*C+24), 4*(H*C+24)), (24, 26, 33)); dr = ImageDraw.Draw(sheet)
        for k, kind in enumerate(('dog', 'cat')):
            for m in range(MOODS):
                px = draw(kind, m, phase=1 if m in (9, 10, 11) else 0).load()
                ox = (m % cols)*(W*C+24) + 12; oy = (k*2 + m // cols)*(H*C+24) + 12
                for y in range(H):
                    for x in range(W):
                        if px[x, y]:
                            dr.ellipse([ox+x*C+1, oy+y*C+1, ox+x*C+C-1, oy+y*C+C-1], fill=(225, 225, 225))
        sheet.save(sys.argv[1])
