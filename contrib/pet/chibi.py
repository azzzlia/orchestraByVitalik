"""Generates ui/pet_chibi_frames.go: small full-height pets (tyan, ОС, puppy, kitten)
in 12 moods, saddest first. Braille, about 18x12 characters — a desktop-pet size.

Run from the repo root: python3 contrib/pet/chibi.py [preview.png]
"""
from PIL import Image, ImageDraw
import math, os, sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from tyan import braille  # noqa: E402

W, H = 36, 48   # px -> 18 cols x 12 rows
S = 8
MOODS = 12
KINDS = ('tyan', 'os', 'dog', 'cat')


def draw(kind, mood, blink=False, dy=0, phase=0, step=None):
    im = Image.new('L', (W*S, H*S), 0); d = ImageDraw.Draw(im)
    lw = int(S*1.05)

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

    def poly(pts, o=None, fill=255):
        d.polygon([P(x, y, o) for x, y in pts], fill=fill)

    def dot(x, y, r=0.6, o=None):
        E((x-r, y-r, x+r, y+r), fill=True, o=o)

    def heart(cx, cy, r=1.8, o=0):
        pts = []
        for i in range(20):
            t = 2*math.pi*i/20
            x = 16*math.sin(t)**3
            y = 13*math.cos(t)-5*math.cos(2*t)-2*math.cos(3*t)-math.cos(4*t)
            pts.append((cx+x*r/16, cy-y*r/16))
        poly(pts, o)

    def sparkle(cx, cy, r=1.6):
        L([(cx-r, cy), (cx+r, cy)], 0); L([(cx, cy-r), (cx, cy+r)], 0)

    def legs(xl, xr, top, bottom, o=None):
        # Standing: both legs down. Walking (step 0/1): one leg lifted with the foot
        # forward, the other on the ground — they swap every step.
        if step is None:
            L([(xl, top), (xl, bottom)], o); L([(xr, top), (xr, bottom)], o)
            return
        up, down = (xl, xr) if step == 0 else (xr, xl)
        L([(down, top), (down, bottom)], o)
        L([(up, top), (up, bottom - 3.5), (up + (2 if up == xr else -2), bottom - 3)], o)

    cx, hy = 18, 18          # head centre
    ey, my = 19, 23          # eyes and mouth
    ex = (14.5, 21.5)

    # --- body (by kind) ---
    if kind == 'tyan':
        # long red hair down to the shoulders, bangs, ahoge, A-line dress, legs
        poly([(9, 30), (8, 18), (11, 11), (18, 8), (25, 11), (28, 18), (27, 30), (24, 30), (25, 18),
              (22, 13), (18, 15), (14, 13), (11, 18), (12, 30)])
        E((10.5, 10.5, 25.5, 28))
        A((16, 5, 20, 9), 200, 340)
        L([(14, 28), (13, 31), (10, 40), (26, 40), (23, 31), (22, 28)])
        L([(10, 33), (8, 36)]); L([(26, 33), (28, 36)])
        legs(15, 21, 40, 46, o=0)
    elif kind == 'os':
        # undercut with receding temples, ears, stubble, jacket with collar and zip
        E((11, 10, 25, 28))
        poly([(11, 15), (11, 11), (14, 8), (18, 7), (22, 8), (25, 11), (25, 15), (23.5, 13), (21, 11.5),
              (18, 13), (15, 11.5), (12.5, 13)])
        L([(11, 18), (9, 18), (9, 21), (11, 22)]); L([(25, 18), (27, 18), (27, 21), (25, 22)])
        for x, y in ((14, 25), (18, 26.5), (22, 25)):
            dot(x, y, 0.5)
        L([(15, 28), (11, 30), (10, 40), (26, 40), (25, 30), (21, 28)])
        L([(15, 28), (14, 31), (18, 33), (22, 31), (21, 28)]); L([(18, 33), (18, 40)])
        L([(10, 32), (8, 38)]); L([(26, 32), (28, 38)])
        legs(14, 22, 40, 46, o=0)
    elif kind == 'dog':
        # puppy sitting: floppy ears, nose, round body, front paws, wagging tail
        E((10, 10, 26, 26))
        poly([(12, 11), (8, 12), (6, 18), (7, 23), (10, 21), (11, 15)])
        poly([(24, 11), (28, 12), (30, 18), (29, 23), (26, 21), (25, 15)])
        E((16.5, 20, 19.5, 22), fill=True)
        E((11, 25, 25, 44))
        legs(15, 21, 34, 44)
        L([(16, 26), (18, 27), (20, 26)])
        t = 1 if phase % 2 else 0
        L([(25, 40), (29, 38 - 2*t), (31, 34 - 2*t)])
        my = 24
    else:
        # kitten sitting: pointed ears, whiskers, round body, tail around the paws
        E((10, 11, 26, 27))
        poly([(11, 15), (11, 6), (16, 11)]); poly([(25, 15), (25, 6), (20, 11)])
        poly([(18, 21), (17, 20), (19, 20)])
        for y0, y1 in ((21, 20), (23, 23.5)):
            L([(12, y0), (6, y1)], w=int(S*0.7)); L([(24, y0), (30, y1)], w=int(S*0.7))
        E((11, 26, 25, 44))
        legs(15, 21, 35, 44)
        L([(25, 42), (30, 42), (32, 38), (31, 34)])
        my = 24

    # --- face ---
    def eyes_open(big=False):
        for x in ex:
            E((x-1.4, ey-1.8, x+1.4, ey+1.8), fill=True)
            if big:
                d.ellipse([P(x-0.6, ey-1.2)[0], P(x-0.6, ey-1.2)[1], P(x+0.3, ey-0.3)[0], P(x+0.3, ey-0.3)[1]], fill=0)

    def eyes(kind_):
        for x in ex:
            if kind_ == 'cry':
                pass
            elif kind_ == 'half':
                L([(x-1.6, ey), (x+1.6, ey)]); dot(x, ey+0.8, 0.9)
            elif kind_ == 'flat':
                L([(x-1.6, ey+0.5), (x+1.6, ey+0.5)])
            elif kind_ == 'closed':
                A((x-1.8, ey-1, x+1.8, ey+2), 20, 160)
            elif kind_ == 'happy':
                A((x-1.8, ey-0.5, x+1.8, ey+2.5), 200, 340)
            elif kind_ == 'star':
                sparkle(x, ey, 1.6)
            elif kind_ == 'heart':
                heart(x, ey, 1.9, dy)
        if kind_ == 'cry':
            L([(ex[0]-1.6, ey-1.5), (ex[0]+1.2, ey), (ex[0]-1.6, ey+1.5)])
            L([(ex[1]+1.6, ey-1.5), (ex[1]-1.2, ey), (ex[1]+1.6, ey+1.5)])

    def brows_sad():
        L([(ex[0]-1.6, ey-3), (ex[0]+1.4, ey-3.8)]); L([(ex[1]-1.4, ey-3.8), (ex[1]+1.6, ey-3)])

    def brows_angry():
        L([(ex[0]-1.6, ey-3.8), (ex[0]+1.4, ey-2.6)]); L([(ex[1]-1.4, ey-2.6), (ex[1]+1.6, ey-3.8)])

    sad_brows = brows_angry if kind == 'os' else brows_sad
    if blink and mood in (2, 3, 5, 6, 7): eyes('closed')
    elif mood == 0: eyes('cry'); sad_brows()
    elif mood == 1: eyes_open(); sad_brows()
    elif mood == 2: eyes('half'); sad_brows()
    elif mood == 3: eyes('half')
    elif mood == 4: eyes('flat')
    elif mood in (5, 6): eyes_open()
    elif mood == 7: eyes_open(True)
    elif mood in (8, 9): eyes('happy')
    elif mood == 10:
        if kind == 'os':   # sunglasses
            poly([(12.5, ey-1.5), (16.5, ey-1.5), (16, ey+1.5), (13, ey+1.5)])
            poly([(19.5, ey-1.5), (23.5, ey-1.5), (23, ey+1.5), (20, ey+1.5)])
            L([(16.5, ey-1), (19.5, ey-1)])
        else:
            eyes('star')
    else:
        if kind == 'os':
            poly([(12.5, ey-1.5), (16.5, ey-1.5), (16, ey+1.5), (13, ey+1.5)])
            poly([(19.5, ey-1.5), (23.5, ey-1.5), (23, ey+1.5), (20, ey+1.5)])
            L([(16.5, ey-1), (19.5, ey-1)])
        else:
            eyes('heart')

    if mood == 0: E((16.5, my-0.5, 19.5, my+2.5))
    elif mood in (1, 2): A((16, my, 20, my+3), 200, 340)
    elif mood == 3: L([(16, my+1), (17.3, my+0.3), (18.7, my+1.5), (20, my+0.8)])
    elif mood == 4: L([(16.5, my+1), (19.5, my+1)])
    elif mood == 5:
        if kind == 'cat': A((15.5, my-1, 18, my+1.5), 0, 180); A((18, my-1, 20.5, my+1.5), 0, 180)
        else: L([(16.5, my+0.5), (19.5, my+0.5)])
    elif mood in (6, 7):
        A((15.5, my-2, 20.5, my+1.5 + (mood == 7)), 20, 160)
    else:
        x0, y0 = P(15.5, my-1); x1, y1 = P(20.5, my+3)
        d.chord([x0, y0, x1, y1], 0, 180, outline=255, width=lw)
        if kind == 'dog' and mood >= 9:
            E((17, my+1.5, 19, my+4.5), fill=True)

    if mood >= 7 and kind != 'os':
        for x in (12.2, 23.8): L([(x-1, my-0.5), (x, my-1)]); L([(x+0.5, my-0.5), (x+1.5, my-1)])

    # --- props around the pet ---
    if mood == 0:
        E((5, 0, 11, 4), o=0); E((9, -1, 15, 4), o=0)
        for x in (7, 10, 13): L([(x, 5+phase), (x, 6.5+phase)], 0)
        for x in (ex[0], ex[1]): L([(x, ey+2+phase/2), (x, ey+4+phase/2)])
    if mood == 1: L([(ex[0], ey+2.5+phase/2), (ex[0], ey+4+phase/2)])
    if mood == 3: L([(29, 12), (29.6, 14.5)], 0)
    if mood == 4: L([(28, 6-phase), (30.5, 6-phase), (28, 8.5-phase), (30.5, 8.5-phase)], 0)
    if mood == 9:
        for i, (x, y) in enumerate(((3, 8), (33, 6), (3, 30), (33, 28))):
            if (i + phase) % 2 == 0: sparkle(x, y)
    if mood == 10:
        sparkle(3, 8, 1.6 + phase % 2); sparkle(33, 6, 2.6 - phase % 2)
        heart(3, 30 - phase, 1.5); heart(33, 28 + phase - 1, 1.5)
    if mood == 11:
        for i, (x, y) in enumerate(((3, 12), (33, 10), (7, 3), (29, 2))):
            heart(x, y - ((phase + i) % 3) * 1.5, 1.8)
    return im.resize((W, H), Image.LANCZOS).point(lambda v: 255 if v > 95 else 0)


def frames(kind, mood):
    b = lambda **kw: braille(draw(kind, mood, **kw))
    if mood in (2, 3, 5, 6, 7):
        a, c = b(phase=0), b(blink=True)
        if kind == 'dog':
            return [b(phase=0), b(phase=1), b(phase=0), b(phase=1), b(phase=0), c]
        return [a, a, a, a, a, c]
    if mood in (0, 1):
        return [b(phase=p) for p in (0, 2, 4)]
    if mood == 4:
        return [b(phase=p) for p in (0, 1, 2, 3)]
    if mood == 8:
        return [b(dy=1, phase=0), b(dy=-1, phase=1)]
    if mood in (9, 10):
        return [b(dy=1, phase=0), b(dy=-1, phase=1)]
    return [b(dy=(1 if p % 2 == 0 else -1), phase=p) for p in range(3)]


def go_file():
    out = ['// Code generated by contrib/pet/chibi.py; DO NOT EDIT.', '', 'package ui', '',
           '// chibiFrames[kind][mood][frame]: small full-height pets, saddest mood first.',
           'var chibiFrames = map[string][][][]string{']
    for kind in KINDS:
        out.append('\t"%s": {' % kind)
        for m in range(MOODS):
            out.append('\t\t{')
            for fr in frames(kind, m):
                out.append('\t\t\t{')
                out += ['\t\t\t\t"%s",' % r.rstrip('⠀') for r in fr]
                out.append('\t\t\t},')
            out.append('\t\t},')
        out.append('\t},')
    out.append('}')
    out += ['', '// chibiWalk[kind][mood] are two walking steps (legs swap), saddest mood first.',
            'var chibiWalk = map[string][][][]string{']
    for kind in KINDS:
        out.append('\t"%s": {' % kind)
        for m in range(MOODS):
            out.append('\t\t{')
            for st, d in ((0, 0), (1, -1)):
                fr = braille(draw(kind, m, step=st, dy=d, phase=1 if m in (9, 10, 11) else 0))
                out.append('\t\t\t{')
                out += ['\t\t\t\t"%s",' % r.rstrip('⠀') for r in fr]
                out.append('\t\t\t},')
            out.append('\t\t},')
        out.append('\t},')
    out.append('}')
    return '\n'.join(out) + '\n'


if __name__ == '__main__':
    open('ui/pet_chibi_frames.go', 'w').write(go_file())
    if len(sys.argv) > 1:
        C = 6
        sheet = Image.new('RGB', (MOODS*(W*C+12), len(KINDS)*(H*C+12)), (24, 26, 33)); dr = ImageDraw.Draw(sheet)
        for k, kind in enumerate(KINDS):
            for m in range(MOODS):
                px = draw(kind, m, phase=1 if m in (9, 10, 11) else 0).load()
                ox = m*(W*C+12) + 6; oy = k*(H*C+12) + 6
                for y in range(H):
                    for x in range(W):
                        if px[x, y]:
                            dr.ellipse([ox+x*C+1, oy+y*C+1, ox+x*C+C-1, oy+y*C+C-1], fill=(225, 225, 225))
        sheet.save(sys.argv[1])
