"""Generates ui/pet_frames.go: the braille tyan in 12 moods, saddest first.

Run from the repo root: python3 contrib/pet/tyan.py [preview.png]
"""
from PIL import Image, ImageDraw
import math, sys

W, H = 52, 64   # px -> 26 cols x 16 rows
S = 6
MOODS = 12


def draw(mood, blink=False, dy=0, phase=0):
    im = Image.new('L', (W*S, H*S), 0); d = ImageDraw.Draw(im)
    lw = int(S*1.1)
    def L(pts, ddy=None):
        o = dy if ddy is None else ddy
        d.line([(x*S, (y+o)*S) for x, y in pts], fill=255, width=lw, joint='curve')
    def A(box, a0, a1):
        d.arc([box[0]*S, (box[1]+dy)*S, box[2]*S, (box[3]+dy)*S], a0, a1, fill=255, width=lw)
    def E(box, fill=False, ddy=None):
        o = dy if ddy is None else ddy
        b = [box[0]*S, (box[1]+o)*S, box[2]*S, (box[3]+o)*S]
        d.ellipse(b, fill=255) if fill else d.ellipse(b, outline=255, width=lw)
    def hole(box):
        d.ellipse([box[0]*S, (box[1]+dy)*S, box[2]*S, (box[3]+dy)*S], fill=0)
    def sparkle(cx, cy, r=2, ddy=0):
        L([(cx-r, cy), (cx+r, cy)], ddy); L([(cx, cy-r), (cx, cy+r)], ddy)
    def heart(cx, cy, r=2.2, ddy=0):
        pts = []
        for i in range(24):
            t = 2*math.pi*i/24
            x = 16*math.sin(t)**3
            y = 13*math.cos(t)-5*math.cos(2*t)-2*math.cos(3*t)-math.cos(4*t)
            pts.append(((cx+x*r/16)*S, (cy-y*r/16+ddy)*S))
        d.polygon(pts, fill=255)
    def star_eye(cx, cy, r=4.5):
        pts = []
        for i in range(10):
            a = -math.pi/2 + i*math.pi/5
            rr = r if i % 2 == 0 else r*0.45
            pts.append(((cx+rr*math.cos(a))*S, (cy+rr*math.sin(a)+dy)*S))
        d.polygon(pts, fill=255)

    # --- head, hair, body (same for every mood) ---
    A((7, 3, 45, 41), 180, 360)
    L([(7, 22), (6, 34), (5, 46), (7, 56), (10, 60)])
    L([(45, 22), (46, 34), (47, 46), (45, 56), (42, 60)])
    L([(12, 22), (11, 36), (12, 48), (14, 54)])
    L([(40, 22), (41, 36), (40, 48), (38, 54)])
    L([(13, 26), (14, 36), (18, 43), (26, 47), (34, 43), (38, 36), (39, 26)])
    L([(27, 6), (22, 11), (17, 17), (14, 24)])
    L([(27, 6), (31, 11), (36, 16), (39, 24)])
    L([(24, 9), (22, 16), (21, 24)])
    L([(29, 9), (31, 16), (33, 23)])
    L([(20, 13), (17, 21), (17, 26)])
    A((24, -2, 32, 6), 200, 340)                       # ahoge
    L([(38, 15), (42, 13)]); L([(39, 12), (41, 16)])   # hair clip
    L([(22, 47), (22, 51)]); L([(30, 47), (30, 51)])
    L([(10, 63), (16, 55), (22, 52), (26, 54), (30, 52), (36, 55), (42, 63)])

    ey, my = 31, 41
    def open_eyes(big=False):
        for cx in (19, 33):
            E((cx-4, ey-5, cx+4, ey+5))
            E((cx-2, ey-2, cx+2, ey+4), fill=True)
            hole((cx-1.5, ey-3.5, cx+0.5, ey-1.5))
            if big:
                hole((cx+0.5, ey+1, cx+1.8, ey+2.3))
            L([(cx-5, ey-5), (cx+5, ey-6)])
    def closed():
        for cx in (19, 33): A((cx-5, ey-3, cx+5, ey+5), 20, 160)
    def happy_eyes():
        for cx in (19, 33): A((cx-4, ey-1, cx+4, ey+5), 200, 340)
    def half_eyes():
        for cx in (19, 33):
            L([(cx-5, ey-1), (cx+5, ey-1)])
            A((cx-4, ey-5, cx+4, ey+5), 0, 180)
            E((cx-2, ey, cx+2, ey+4), fill=True)
    def flat_eyes():
        for cx in (19, 33): L([(cx-4, ey+1), (cx+4, ey+1)])
    def cry_eyes():
        L([(15, ey-3), (22, ey), (15, ey+3)]); L([(37, ey-3), (30, ey), (37, ey+3)])
    def brows_sad():
        L([(14, ey-8), (22, ey-10)]); L([(30, ey-10), (38, ey-8)])
    def blush():
        for cx in (13, 39): L([(cx-2, 38), (cx, 37)]); L([(cx+1, 38), (cx+3, 37)])

    blinks = mood in (2, 3, 4, 5, 6, 7)
    # eyes
    if blink and blinks: closed()
    elif mood == 0: cry_eyes(); brows_sad()
    elif mood == 1: open_eyes(); brows_sad()
    elif mood == 2: half_eyes(); brows_sad()
    elif mood == 3: half_eyes()
    elif mood == 4: flat_eyes()
    elif mood == 5: open_eyes()
    elif mood == 6: open_eyes()
    elif mood == 7: open_eyes(True)
    elif mood in (8, 9): happy_eyes()
    elif mood == 10:
        for cx in (19, 33): star_eye(cx, ey+1)
    elif mood == 11:
        for cx in (19, 33): heart(cx, ey+1, 4.6, dy)

    # mouth
    if mood == 0: E((22, my-2, 30, my+5))                       # wailing
    elif mood in (1, 2): A((23, my, 29, my+5), 200, 340)        # frown
    elif mood == 3:                                              # wavy
        L([(23, my+1), (25, my), (27, my+2), (29, my+1)])
    elif mood == 4: L([(24, my+1), (28, my+1)])                 # flat
    elif mood == 5: L([(24, my), (28, my)])                     # neutral
    elif mood == 6: A((23, my-3, 29, my+1), 30, 150)            # smile
    elif mood == 7: A((22, my-4, 30, my+2), 20, 160)            # big smile
    elif mood in (8, 9, 10):
        d.chord([22*S, (my-3+dy)*S, 30*S, (my+4+dy)*S], 0, 180, outline=255, width=lw)
    elif mood == 11:
        d.chord([21*S, (my-4+dy)*S, 31*S, (my+5+dy)*S], 0, 180, outline=255, width=lw)

    # extras and animation
    if mood >= 7: blush()
    if mood == 0:   # rain cloud and two streams of tears
        E((3, 2, 11, 8), ddy=0); E((8, 0, 16, 7), ddy=0); E((12, 3, 19, 8), ddy=0)
        for x in (7, 12, 16): L([(x, 10+phase), (x, 12+phase)], 0)
        for x in (16, 36): L([(x, ey+3+phase), (x, ey+9+phase)])
    if mood == 1: L([(19, ey+6+phase), (19, ey+9+phase)])
    if mood == 2: L([(33, ey+6+phase), (33, ey+8+phase)])
    if mood == 3: L([(41, 18), (42, 22)])                       # sweat drop
    if mood == 4:                                                # zzz
        L([(43, 6-phase), (46, 6-phase), (43, 9-phase), (46, 9-phase)], 0)
    if mood == 9:
        for i, (cx, cy) in enumerate(((4, 10), (48, 8), (3, 26), (49, 24))):
            if (i + phase) % 2 == 0: sparkle(cx, cy)
    if mood == 10:
        for i, (cx, cy) in enumerate(((4, 10), (48, 8))):
            sparkle(cx, cy, 2 + (i + phase) % 2)
        heart(4, 24 - phase, 1.8); heart(48, 22 + phase - 2, 1.8)
    if mood == 11:
        for i, (cx, cy) in enumerate(((4, 14), (48, 12), (8, 4), (44, 3))):
            heart(cx, cy - ((phase + i) % 3) * 2, 2.2)
        sparkle(2, 30, 2); sparkle(50, 30, 2)
    return im.resize((W, H), Image.LANCZOS).point(lambda v: 255 if v > 90 else 0)


def braille(im):
    w, h = im.size; px = im.load(); out = []
    bits = [(0, 0, 0x1), (0, 1, 0x2), (0, 2, 0x4), (1, 0, 0x8), (1, 1, 0x10), (1, 2, 0x20), (0, 3, 0x40), (1, 3, 0x80)]
    for y in range(0, h, 4):
        row = ''
        for x in range(0, w, 2):
            c = 0
            for dx, ddy, b in bits:
                if x+dx < w and y+ddy < h and px[x+dx, y+ddy]: c |= b
            row += chr(0x2800+c)
        out.append(row)
    return out


def frames(mood):
    if mood in (2, 3, 5, 6, 7):
        a = braille(draw(mood)); b = braille(draw(mood, blink=True))
        f = [a, a, a, a, a, b]
        if mood == 2:
            f = [braille(draw(2, phase=p)) for p in (0, 0, 2, 2)] + [b]
        return f
    if mood in (0, 1):
        return [braille(draw(mood, phase=p)) for p in (0, 2, 4)]
    if mood == 4:
        return [braille(draw(4, phase=p)) for p in (0, 1, 2, 3)]
    if mood == 8:
        return [braille(draw(8, dy=1)), braille(draw(8, dy=-1))]
    if mood in (9, 10):
        return [braille(draw(mood, dy=1, phase=0)), braille(draw(mood, dy=-1, phase=1))]
    return [braille(draw(11, dy=(1 if p % 2 == 0 else -1), phase=p)) for p in range(3)]


def go_file(allf):
    out = ['// Code generated by contrib/pet/tyan.py; DO NOT EDIT.', '', 'package ui', '',
           '// tyanFrames[mood][frame] is the braille line art of the pet, saddest mood first.',
           'var tyanFrames = [][][]string{']
    for mf in allf:
        out.append('\t{')
        for fr in mf:
            out.append('\t\t{')
            out += ['\t\t\t"%s",' % r.rstrip('⠀') for r in fr]
            out.append('\t\t},')
        out.append('\t},')
    out.append('}')
    return '\n'.join(out) + '\n'


if __name__ == '__main__':
    allf = [frames(m) for m in range(MOODS)]
    open('ui/pet_frames.go', 'w').write(go_file(allf))
    if len(sys.argv) > 1:
        cols = 6
        sheet = Image.new('L', (cols*(W+6)*4, 2*(H+6)*4), 0)
        for m in range(MOODS):
            ph = 1 if m in (9, 10, 11) else 0
            im = draw(m, phase=ph).resize((W*4, H*4), Image.NEAREST)
            sheet.paste(im, ((m % cols)*(W+6)*4, (m // cols)*(H+6)*4))
        sheet.save(sys.argv[1])
