"""Makes the load test's pictures in DIR: 50 photos of 12 megapixels
(jpg/), and a PDF that is one 12-megapixel scan (scan-12mp.pdf)."""
import os, random, sys
from PIL import Image, ImageDraw

out = sys.argv[1]
os.makedirs(os.path.join(out, "jpg"), exist_ok=True)
random.seed(7)


def picture():
    im = Image.new("RGB", (4032, 3024), (random.randrange(256),) * 3)
    d = ImageDraw.Draw(im)
    for _ in range(400):
        x, y = random.randrange(4032), random.randrange(3024)
        d.ellipse((x, y, x + random.randrange(40, 600), y + random.randrange(40, 600)),
                  fill=(random.randrange(256), random.randrange(256), random.randrange(256)))
    return im


for n in range(50):
    p = os.path.join(out, "jpg", f"IMG_{3000 + n}.jpg")
    if not os.path.exists(p):
        picture().save(p, quality=88)
p = os.path.join(out, "scan-12mp.pdf")
if not os.path.exists(p):
    picture().save(p, "PDF", resolution=300, quality=85)
