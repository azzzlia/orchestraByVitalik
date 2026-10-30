"""Generates ui/pet_os_frames.go: «ОС», the manager pet, in 12 moods (grumpiest first).

Short undercut with a swept-back top, slightly receding temples, stubble, an ear stud
and a high jacket collar. The less density, the more he nags; the more, the happier.

Run from the repo root: python3 contrib/pet/os.py [preview.png]
"""
from PIL import Image, ImageDraw
import math, os, sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from tyan import braille  # noqa: E402

W, H = 52, 64   # px -> 26 cols x 16 rows
S = 6
MOODS = 12


def draw(mood, blink=False, dy=0, dx=0, phase=0):
    im = Image.new('L', (W*S, H*S), 0); d = ImageDraw.Draw(im)
    lw = int(S*1.1)

    def P(x, y, o=None):
        return ((x+dx)*S, (y+(dy if o is None else o))*S)

    def L(pts, o=None, w=lw):
        d.line([P(x, y, o) for x, y in pts], fill=255, width=w, joint='curve')

    def A(box, a0, a1, o=None):
        x0, y0 = P(box[0], box[1], o); x1, y1 = P(box[2], box[3], o)
        d.arc([x0, y0, x1, y1], a0, a1, fill=255, width=lw)

    def E(box, fill=False, o=None):
        x0, y0 = P(box[0], box[1], o); x1, y1 = P(box[2], box[3], o)
        d.ellipse([x0, y0, x1, y1], fill=255) if fill else d.ellipse([x0, y0, x1, y1], outline=255, width=lw)

    def dot(x, y, o=None, r=0.55):
        E((x-r, y-r, x+r, y+r), fill=True, o=o)

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

    # --- head (same for every mood) ---
    # skull and jaw: a strong, slightly square chin
    L([(13, 18), (12, 26), (13, 34), (15, 40), (19, 45), (23, 48), (29, 48), (33, 45), (37, 40), (39, 34), (40, 26), (39, 18)])
    # ears, the left one with a stud
    L([(12, 26), (9, 25), (8, 28), (9, 32), (12, 34)])
    L([(40, 26), (43, 25), (44, 28), (43, 32), (40, 34)])
    dot(9.5, 34.5, r=0.9)
    # undercut: shaved sides shown as stubble dots above the ears
    for y in (17, 20, 23):
        for x in (14, 38):
            dot(x, y)
    # swept-back top with receding temples: the hairline dips to a peak in the middle
    # and goes back at the temples, leaving bare corners of forehead
    poly([(13, 17), (13, 10), (17, 5), (23, 2.5), (30, 2.5), (35, 4.5), (39, 10), (39, 17),
          (37, 16), (35, 9.5), (31, 11), (26, 13.5), (21, 11), (17, 9.5), (15, 16)])
    # comb lines so the top reads as hair swept back, not a helmet
    for x0, x1 in ((17, 21), (22, 25), (27, 29), (32, 33)):
        d.line([P(x0+1, 10.5 if x0 in (17, 32) else 11.5), P(x1, 4)], fill=0, width=int(S*0.9))
    # neck and the high jacket collar
    L([(20, 47), (19, 52)]); L([(32, 47), (33, 52)])
    L([(19, 52), (12, 54), (8, 63)]); L([(33, 52), (40, 54), (44, 63)])
    L([(19, 52), (16, 49), (14, 56), (21, 63)]); L([(33, 52), (36, 49), (38, 56), (31, 63)])
    L([(26, 56), (26, 63)])  # zipper

    ey, my, by = 28, 40, 23   # eye, mouth and brow lines

    # stubble on the chin, cheeks and upper lip
    for x, y in ((17, 38), (35, 38), (19, 42), (33, 42), (22, 45), (26, 46), (30, 45)):
        dot(x, y)
    # nose
    L([(26, 28), (25, 33), (27, 34)])

    def brows(kind):
        if kind == 'angry':      # down to the nose
            L([(16, by-1), (23, by+2)], w=int(S*1.6)); L([(36, by-1), (29, by+2)], w=int(S*1.6))
        elif kind == 'skeptic':  # one up, one down
            L([(16, by-3), (23, by-2)], w=int(S*1.6)); L([(29, by+1), (36, by)], w=int(S*1.6))
        elif kind == 'flat':
            L([(16, by), (23, by)], w=int(S*1.6)); L([(29, by), (36, by)], w=int(S*1.6))
        elif kind == 'up':
            L([(16, by), (19, by-2), (23, by-1)], w=int(S*1.6)); L([(29, by-1), (33, by-2), (36, by)], w=int(S*1.6))

    def eyes(kind):
        for cx in (19.5, 32.5):
            if kind == 'narrow':
                L([(cx-3, ey), (cx+3, ey)]); dot(cx, ey+1, r=1)
            elif kind == 'open':
                A((cx-3, ey-2, cx+3, ey+2), 180, 360); dot(cx, ey+0.5, r=1.2)
            elif kind == 'closed':
                L([(cx-3, ey+1), (cx+3, ey+1)])
            elif kind == 'happy':
                A((cx-3, ey-1, cx+3, ey+4), 200, 340)
            elif kind == 'wide':
                E((cx-3, ey-2.5, cx+3, ey+2.5)); dot(cx, ey, r=1)

    def shades():
        poly([(15, ey-3), (24, ey-3), (23, ey+3), (16, ey+3)])
        poly([(28, ey-3), (37, ey-3), (36, ey+3), (29, ey+3)])
        L([(24, ey-2), (28, ey-2)])
        L([(15, ey-2), (12, ey-3)]); L([(37, ey-2), (40, ey-3)])

    # --- face by mood ---
    if mood == 0:   # shouting
        brows('angry'); eyes('narrow')
        E((22, my-2, 30, my+4))
        L([(16, 9), (18, 11)], 0); L([(18, 9), (16, 11)], 0)          # vein mark
        L([(46, 8), (46, 13)], 0); dot(46, 15.5, o=0, r=0.9)          # !
        L([(49, 8), (49, 13)], 0); dot(49, 15.5, o=0, r=0.9)          # !
    elif mood == 1:  # angry
        brows('angry'); eyes('narrow')
        A((22, my, 30, my+5), 200, 340)
        L([(47, 9), (47, 14)], 0); dot(47, 16.5, o=0, r=0.9)          # !
    elif mood == 2:  # "Как дела?"
        brows('skeptic'); eyes('narrow')
        L([(22, my+1), (30, my)])
        A((44, 6, 50, 12), 180, 400, 0); L([(47, 12), (47, 14)], 0); dot(47, 16.5, o=0, r=0.9)  # ?
    elif mood == 3:  # tapping the watch
        brows('flat'); eyes('narrow')
        L([(22, my+1), (25, my), (27, my+1), (30, my)])
        E((43, 7, 50, 14), o=0); L([(46.5, 10.5), (46.5, 8.5)], 0); L([(46.5, 10.5), (48, 11.5)], 0)  # clock
    elif mood == 4:  # stern
        brows('flat'); eyes('closed' if blink else 'narrow')
        L([(23, my+1), (29, my+1)])
    elif mood == 5:  # almost ok
        brows('flat'); eyes('closed' if blink else 'open')
        L([(23, my+1), (28, my+1), (30, my)])
    elif mood == 6:  # goal met: a small nod
        brows('up'); eyes('closed' if blink else 'open')
        A((22, my-3, 30, my+2), 30, 150)
    elif mood == 7:
        brows('up'); eyes('closed' if blink else 'open')
        A((21, my-4, 31, my+2), 20, 160)
    elif mood == 8:
        brows('up'); eyes('happy')
        d.chord([P(21, my-3)[0], P(21, my-3)[1], P(31, my+4)[0], P(31, my+4)[1]], 0, 180, outline=255, width=lw)
    elif mood == 9:  # thumbs up
        brows('up'); eyes('happy')
        d.chord([P(21, my-3)[0], P(21, my-3)[1], P(31, my+4)[0], P(31, my+4)[1]], 0, 180, outline=255, width=lw)
        L([(43, 22), (43, 15), (45, 11), (46, 15), (50, 15), (50, 22), (43, 22)], 0)
        for i, (cx, cy) in enumerate(((5, 10), (4, 24))):
            if (i + phase) % 2 == 0:
                sparkle(cx, cy)
    elif mood == 10:  # sunglasses
        brows('up'); shades()
        d.chord([P(21, my-3)[0], P(21, my-3)[1], P(31, my+4)[0], P(31, my+4)[1]], 0, 180, outline=255, width=lw)
        for i, (cx, cy) in enumerate(((5, 10), (47, 8), (4, 24), (48, 22))):
            sparkle(cx, cy, 2 + (i + phase) % 2)
    elif mood == 11:  # sunglasses, big grin, hearts
        brows('up'); shades()
        d.chord([P(20, my-4)[0], P(20, my-4)[1], P(32, my+5)[0], P(32, my+5)[1]], 0, 180, outline=255, width=lw)
        for i, (cx, cy) in enumerate(((4, 14), (48, 12), (7, 4), (45, 3))):
            heart(cx, cy - ((phase + i) % 3) * 2, 2.2)
    return im.resize((W, H), Image.LANCZOS).point(lambda v: 255 if v > 90 else 0)


def frames(mood):
    if mood == 0:   # shaking with anger
        return [braille(draw(0, dx=x)) for x in (0, 1, 0, -1)]
    if mood in (1, 2, 3):
        return [braille(draw(mood, phase=p)) for p in (0, 0, 0, 1)]
    if mood in (4, 5, 6, 7):
        a = braille(draw(mood)); b = braille(draw(mood, blink=True))
        return [a, a, a, a, a, b]
    if mood == 8:
        return [braille(draw(8, dy=1)), braille(draw(8, dy=-1))]
    if mood in (9, 10):
        return [braille(draw(mood, dy=1, phase=0)), braille(draw(mood, dy=-1, phase=1))]
    return [braille(draw(11, dy=(1 if p % 2 == 0 else -1), phase=p)) for p in range(3)]


def go_file(allf):
    out = ['// Code generated by contrib/pet/os.py; DO NOT EDIT.', '', 'package ui', '',
           '// osFrames[mood][frame] is the braille line art of «ОС», grumpiest mood first.',
           'var osFrames = [][][]string{']
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
    open('ui/pet_os_frames.go', 'w').write(go_file(allf))
    if len(sys.argv) > 1:
        cols = 6
        sheet = Image.new('L', (cols*(W+6)*4, 2*(H+6)*4), 0)
        for m in range(MOODS):
            ph = 1 if m in (9, 10, 11) else 0
            im = draw(m, phase=ph).resize((W*4, H*4), Image.NEAREST)
            sheet.paste(im, ((m % cols)*(W+6)*4, (m // cols)*(H+6)*4))
        sheet.save(sys.argv[1])
