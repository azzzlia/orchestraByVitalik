"""Generates AppIcon.icon — the macOS 26 icon format (Icon Composer): a dark fill
and the art as one flat layer. sq compiles it with Xcode's actool into Assets.car,
so the Dock shows it like any other app instead of on a gray plate.
Run: python3 icon_composer.py"""
from PIL import Image, ImageDraw
import json, math, os

S = 1024
im = Image.new("RGBA", (S, S), (0, 0, 0, 0))
d = ImageDraw.Draw(im)
k = S / 2048
cx, cy = S // 2, int(S * 0.66)
for r, a in ((380, 120), (600, 95), (820, 75)):
    r = r * k * 0.92
    d.arc([cx - r, cy - r, cx + r, cy + r], 180, 360, fill=(236, 226, 205, 255 - a), width=int(18 * k))
dots = [(820, 200, "#4ade80"), (820, 235, "#38bdf8"), (820, 270, "#f5b942"), (820, 305, "#a78bfa"), (820, 340, "#f2667a"),
        (600, 215, "#4ade80"), (600, 270, "#38bdf8"), (600, 325, "#fb923c"), (380, 245, "#ec6f9c"), (380, 295, "#5ad1e6")]
for r, ang, col in dots:
    a = math.radians(ang)
    rr = r * k * 0.92
    x, y = cx + rr * math.cos(a), cy + rr * math.sin(a)
    rad = (78 if r == 820 else 62 if r == 600 else 52) * k
    d.ellipse([x - rad, y - rad, x + rad, y + rad], fill=col)
s = k
d.line([(cx + 60 * s, cy - 40 * s), (cx + 330 * s, cy - 250 * s)], fill="#d6a447", width=int(34 * s))
d.ellipse([cx - 150 * s, cy - 150 * s, cx + 150 * s, cy + 150 * s], fill="#f1eff7")
d.polygon([(cx - 70 * s, cy - 20 * s), (cx - 45 * s, cy - 80 * s), (cx, cy - 35 * s), (cx + 45 * s, cy - 80 * s), (cx + 70 * s, cy - 20 * s)], fill="#d6a447")

os.makedirs("AppIcon.icon/Assets", exist_ok=True)
im.save("AppIcon.icon/Assets/art.png")
spec = {
    "fill": {"automatic-gradient": "srgb:0.11765,0.08627,0.18039,1.00000"},
    "groups": [{
        "layers": [{"glass": False, "image-name": "art.png", "name": "art"}],
        "shadow": {"kind": "neutral", "opacity": 0.5},
        "translucency": {"enabled": False, "value": 0.5},
    }],
    "supported-platforms": {"circles": ["watchOS"], "squares": "shared"},
}
with open("AppIcon.icon/icon.json", "w") as f:
    json.dump(spec, f, indent=2)
print("ok")
