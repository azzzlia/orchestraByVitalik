"""Generates AppIcon.icns (and icon.png) for the Orchestra app: the conductor and
his orchestra — arcs of colored sessions around a baton. Run: python3 icon.py"""
from PIL import Image, ImageDraw
import math

S = 2048
im = Image.new("RGBA", (S, S), (0, 0, 0, 0))
d = ImageDraw.Draw(im)
# background: rounded square, plum gradient
bg = Image.new("RGBA", (S, S))
bd = ImageDraw.Draw(bg)
for y in range(S):
    t = y / S
    c = (int(30 - 14 * t), int(22 - 10 * t), int(46 - 20 * t), 255)
    bd.line([(0, y), (S, y)], fill=c)
im.paste(bg, (0, 0))
d = ImageDraw.Draw(im)
cx, cy = S // 2, int(S * 0.70)
# arcs
for r, a in ((380, 90), (600, 70), (820, 55)):
    d.arc([cx - r, cy - r, cx + r, cy + r], 180, 360, fill=(236, 226, 205, a), width=18)
# sessions: colored dots on the arcs
dots = [(820, 200, "#4ade80"), (820, 235, "#38bdf8"), (820, 270, "#f5b942"), (820, 305, "#a78bfa"), (820, 340, "#f2667a"),
        (600, 215, "#4ade80"), (600, 270, "#38bdf8"), (600, 325, "#fb923c"), (380, 245, "#ec6f9c"), (380, 295, "#5ad1e6")]
for r, ang, col in dots:
    a = math.radians(ang)
    x, y = cx + r * math.cos(a), cy + r * math.sin(a)
    rad = 78 if r == 820 else 62 if r == 600 else 52
    d.ellipse([x - rad, y - rad, x + rad, y + rad], fill=col)
# the conductor and his baton
d.line([(cx + 60, cy - 40), (cx + 330, cy - 250)], fill="#d6a447", width=34)
d.ellipse([cx - 150, cy - 150, cx + 150, cy + 150], fill="#f1eff7")
d.polygon([(cx - 70, cy - 20), (cx - 45, cy - 80), (cx, cy - 35), (cx + 45, cy - 80), (cx + 70, cy - 20)], fill="#d6a447")
# macOS 26 does not round old-style icons itself: an icon that is not the standard
# squircle (Big Sur grid: an 824/1024 body with a transparent margin) is put on a
# gray plate. So the art goes into that squircle (superellipse, n=5).
B = int(S * 824 / 1024)
art = im.resize((B, B), Image.LANCZOS)
sq = Image.new("L", (B * 2, B * 2), 0)
pix = sq.load()
n, h = 5.0, B
for y in range(B * 2):
    v = abs((y + 0.5) / h - 1) ** n
    if v >= 1:
        continue
    w = (1 - v) ** (1 / n)
    x0, x1 = int(round((1 - w) * h)), int(round((1 + w) * h))
    for x in range(x0, x1):
        pix[x, y] = 255
sq = sq.resize((B, B), Image.LANCZOS)
out = Image.new("RGBA", (S, S), (0, 0, 0, 0))
off = (S - B) // 2
out.paste(art, (off, off), sq)
im = out
icon = im.resize((1024, 1024), Image.LANCZOS)
icon.save("icon.png")
icon.save("AppIcon.icns", sizes=[(16, 16), (32, 32), (64, 64), (128, 128), (256, 256), (512, 512), (1024, 1024)])
print("ok")
