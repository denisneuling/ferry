# Draws ferry's app icon from the mark (ui/src/app/ui/mark.ts): the boat on a
# wave, white and orange on a petrol tile, its lines thicker than the mark's
# so that it reads at 16 pixels. Writes favicon.svg and renders favicon.ico
# (16, 32, 48), the touch icon and the manifest's icons through Chromium, so
# no image library is needed:
#
#   python3 hack/icons/render.py ui/public    # needs playwright and its chromium
#
# The icons in ui/public are its output, committed.
import base64, struct, sys
from playwright.sync_api import sync_playwright

PETROL, WHITE, WAVE = "#0f5f5c", "#ffffff", "#f0a070"

def svg(tile=True, pad=1.0, stroke=2.8):
    # The mark spans x 3..29 and y 7.5..28.5; it is centred and scaled into
    # the tile, smaller (pad < 1) for the maskable icon's safe zone.
    s = 0.94 * pad
    g = f'transform="translate(16 16.4) scale({s}) translate(-16 -18)"'
    bg = f'<rect width="32" height="32" rx="7" fill="{PETROL}"/>' if tile else ""
    if pad < 1:  # maskable: the platform cuts the shape, so the tile is full
        bg = f'<rect width="32" height="32" fill="{PETROL}"/>'
    return f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32" role="img" aria-label="ferry">{bg}<g {g} fill="none" stroke-linecap="round" stroke-linejoin="round"><path d="M4.5 16.5h23l-3.2 6.1a2 2 0 0 1-1.77 1.07H9.47a2 2 0 0 1-1.77-1.07Z" fill="{WHITE}"/><path d="M10 16.5v-3.8a1.2 1.2 0 0 1 1.2-1.2h7.3l3 5" stroke="{WHITE}" stroke-width="{stroke}"/><path d="M15 11.5V7.5" stroke="{WHITE}" stroke-width="{stroke}"/><path d="M3 27.5c2.2 0 2.2-1.6 4.4-1.6s2.2 1.6 4.4 1.6 2.2-1.6 4.4-1.6 2.2 1.6 4.4 1.6 2.2-1.6 4.4-1.6 2.2 1.6 4.4 1.6" stroke="{WAVE}" stroke-width="{stroke}"/></g></svg>'''

def uri(markup):
    return "data:image/svg+xml;base64," + base64.b64encode(markup.encode()).decode()

def render(page, markup, size, transparent=True):
    page.set_viewport_size({"width": size, "height": size})
    page.set_content(f'<html><body style="margin:0;background:transparent">'
                     f'<img src="{uri(markup)}" width="{size}" height="{size}" style="display:block"></body></html>')
    page.wait_for_function("() => document.images[0].complete")
    return page.screenshot(omit_background=transparent, type="png")

def ico(pngs):
    # An ICO whose images are PNGs, as every current browser reads them.
    head = struct.pack("<HHH", 0, 1, len(pngs))
    entries, data, offset = b"", b"", 6 + 16 * len(pngs)
    for size, png in pngs:
        entries += struct.pack("<BBBBHHII", size % 256, size % 256, 0, 0, 1, 32, len(png), offset)
        data += png
        offset += len(png)
    return head + entries + data

out = sys.argv[1]
with sync_playwright() as p:
    b = p.chromium.launch()
    page = b.new_page()
    open(f"{out}/favicon.svg", "w").write(svg() + "\n")
    open(f"{out}/favicon.ico", "wb").write(ico([(s, render(page, svg(), s)) for s in (16, 32, 48)]))
    # The touch icon has no transparency: iOS fills it black.
    open(f"{out}/apple-touch-icon.png", "wb").write(render(page, svg(pad=0.8), 180, transparent=False))
    open(f"{out}/icon-192.png", "wb").write(render(page, svg(), 192))
    open(f"{out}/icon-512.png", "wb").write(render(page, svg(), 512))
    open(f"{out}/icon-maskable-512.png", "wb").write(render(page, svg(pad=0.72), 512, transparent=False))
    # A preview, not for ui/public: the sizes a tab and a home screen show,
    # on light and dark.
    sheet = "".join(f'<div style="background:{bg};padding:10px;display:flex;gap:14px;align-items:center">'
                    + "".join(f'<img src="{uri(svg())}" width="{s}" height="{s}">' for s in (16, 24, 32, 64))
                    + f'<img src="{uri(svg(pad=0.72))}" width="64" height="64" style="border-radius:50%"></div>'
                    for bg in ("#f4f1ec", "#ffffff", "#202124", "#35363a"))
    page.set_viewport_size({"width": 260, "height": 360})
    page.set_content(f"<html><body style='margin:0'>{sheet}</body></html>")
    page.wait_for_timeout(200)
    page.screenshot(path=sys.argv[2] if len(sys.argv) > 2 else "/dev/null")
    b.close()
