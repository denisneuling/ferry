#!/usr/bin/env python3
"""The screenshots in docs/images, made from the mock NAS.

    make dev-nas            # the mock NAS on 127.0.0.1:4455
    make screenshots        # builds the interface, then runs this

It starts ferry itself against the mock NAS, on a port of its own, and
drives Chromium through Playwright (pip install playwright pillow; playwright
install chromium). Everything a picture shows is fixed, so that the pictures
change only when the interface does:

- the data are the mock NAS's, generated from a fixed seed — fictional
  people, files and cameras only, since the repository is public;
- the browser's clock is pinned (greetings, "yesterday"), its time zone,
  language, viewport and motion are set;
- the free space, which would be the free space of whatever disk the mock
  NAS happens to run on, is answered with a fixed figure here: it is the
  only thing the pictures do not take from ferry.

The pictures are written as WebP, small enough to commit.
"""

import io
import os
import pathlib
import socket
import subprocess
import sys
import time
import urllib.request
from datetime import datetime, timezone

from PIL import Image
from playwright.sync_api import sync_playwright

ROOT = pathlib.Path(__file__).resolve().parents[2]
OUT = ROOT / "docs" / "images"
NAS = os.environ.get("FERRY_SCREENSHOT_NAS", "127.0.0.1:4455")
# 10:30 in Berlin on a weekday after the mock's newest files: "Good morning".
NOW = datetime(2026, 11, 12, 9, 30, tzinfo=timezone.utc)
SPACE = '{"total": 8000000000000, "free": 3150000000000}'
USER, PASSWORD = "alex", "alex-nas-demo"  # the mock NAS's demo account

DESKTOP = {"width": 1280, "height": 800}
PHONE = {"width": 390, "height": 844}


def free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def start_ferry():
    port, ops = free_port(), free_port()
    env = dict(os.environ, FERRY_SMB_SERVER=NAS, FERRY_SERVER_NAME="nas.example.lan",
               FERRY_LISTEN=f"127.0.0.1:{port}", FERRY_OPS_LISTEN=f"127.0.0.1:{ops}",
               FERRY_LOG_LEVEL="warn",
               # Every picture signs in afresh, more often than the login
               # rate a person is allowed.
               FERRY_LOGIN_RATE_USER="1000", FERRY_LOGIN_RATE_ADDRESS="1000")
    # Built and run as a binary rather than with go run, whose child would
    # outlive a terminate.
    binary = ROOT / "bin" / "ferry-screenshots"
    subprocess.run(["go", "build", "-o", str(binary), "./cmd/ferry"], cwd=ROOT, check=True)
    # The previews' compiled code, as the image ships it: without it PDF
    # tiles would show an icon instead of their first page.
    wasm = ROOT / "bin" / "ferry-screenshots-wasm"
    subprocess.run([str(binary), "precompile", str(wasm)], cwd=ROOT, check=True)
    env["FERRY_WASM_DIR"] = str(wasm)
    proc = subprocess.Popen([str(binary), "serve"], cwd=ROOT, env=env)
    for _ in range(240):
        try:
            urllib.request.urlopen(f"http://127.0.0.1:{ops}/readyz", timeout=1)
            return proc, f"http://127.0.0.1:{port}"
        except Exception:
            time.sleep(0.5)
    proc.terminate()
    sys.exit("ferry did not become ready: is the mock NAS running (make dev-nas)?")


def save(png: bytes, name: str):
    img = Image.open(io.BytesIO(png)).convert("RGB")
    img.save(OUT / f"{name}.webp", "WEBP", quality=82, method=6)
    print(f"docs/images/{name}.webp {img.width}×{img.height} {(OUT / f'{name}.webp').stat().st_size // 1024} KiB")


def settled(page):
    """Waits until the page is still: requests done, images decoded."""
    page.wait_for_load_state("networkidle")
    page.evaluate("document.fonts.ready")
    # The mouse rests where nothing reacts to it, so no tile shows its hover.
    if page.viewport_size["width"] >= 500:  # a phone has no mouse
        page.mouse.move(page.viewport_size["width"] - 6, page.viewport_size["height"] - 6)
    page.wait_for_function(
        "() => [...document.images].every(i => i.complete && (i.naturalWidth > 0 || !i.src))")
    page.wait_for_timeout(300)


class Shooter:
    def __init__(self, browser, base):
        self.browser, self.base = browser, base

    def context(self, lang, viewport=DESKTOP, dark=False, phone=False):
        ctx = self.browser.new_context(
            viewport=viewport, device_scale_factor=2 if phone else 1, is_mobile=phone, has_touch=phone,
            locale="de-DE" if lang == "de" else "en-GB", timezone_id="Europe/Berlin",
            color_scheme="dark" if dark else "light", reduced_motion="reduce")
        ctx.clock.install(time=NOW)
        ctx.route("**/api/v1/shares/*/space*", lambda r: r.fulfill(
            status=200, content_type="application/json", body=SPACE))
        return ctx

    def login(self, page, lang):
        page.goto(f"{self.base}/{lang}/")
        page.locator("input").nth(0).fill(USER)
        page.locator("input[type=password]").fill(PASSWORD)
        page.locator("button[type=submit]").click()
        # The sign-in button sat where a share's card comes; leave before the
        # card's hover starts.
        if page.viewport_size["width"] >= 500:
            page.mouse.move(page.viewport_size["width"] - 6, page.viewport_size["height"] - 6)
        page.wait_for_selector(".share .space")

    def login_page(self, lang):
        ctx = self.context(lang)
        page = ctx.new_page()
        page.goto(f"{self.base}/{lang}/")
        page.wait_for_selector("button[type=submit]")
        settled(page)
        save(page.screenshot(animations="disabled", caret="hide"), f"login-{lang}")
        ctx.close()

    def shares(self, lang):
        ctx = self.context(lang)
        page = ctx.new_page()
        self.login(page, lang)
        settled(page)
        save(page.screenshot(animations="disabled", caret="hide"), f"shares-{lang}")
        ctx.close()

    def folder(self, lang, dark=False, phone=False, name=None):
        ctx = self.context(lang, PHONE if phone else DESKTOP, dark=dark, phone=phone)
        page = ctx.new_page()
        self.login(page, lang)
        page.goto(f"{self.base}/{lang}/browse/alex?path=Photos%2F2025%20Sommer%20am%20See")
        page.wait_for_selector(".tile img")
        settled(page)
        save(page.screenshot(animations="disabled", caret="hide"), name or f"folder-{lang}")
        ctx.close()

    def scans(self, lang):
        """A folder of PDFs, each tile showing the first page."""
        ctx = self.context(lang)
        page = ctx.new_page()
        self.login(page, lang)
        page.goto(f"{self.base}/{lang}/browse/Scans")
        page.wait_for_selector(".tile img")
        settled(page)
        save(page.screenshot(animations="disabled", caret="hide"), f"scans-{lang}")
        ctx.close()

    def lightbox(self, lang, phone=False, name=None):
        ctx = self.context(lang, PHONE if phone else DESKTOP, phone=phone)
        page = ctx.new_page()
        self.login(page, lang)
        page.goto(f"{self.base}/{lang}/browse/alex?path=Photos%2F2025%20Sommer%20am%20See")
        page.wait_for_selector(".tile img")
        page.locator(".tile", has_text="IMG_2117.jpg").first.click()
        page.wait_for_selector(".lightbox")
        if not phone:
            page.keyboard.press("i")
            page.wait_for_selector(".lb-info-sub")
        settled(page)
        save(page.screenshot(animations="disabled", caret="hide"), name or f"lightbox-{lang}")
        ctx.close()


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    proc, base = start_ferry()
    try:
        with sync_playwright() as p:
            # Rendering without the GPU, partial raster or runtime-tuned Skia,
            # in one colour profile: the same page makes the same pixels.
            browser = p.chromium.launch(args=[
                "--disable-gpu", "--force-color-profile=srgb", "--disable-partial-raster",
                "--disable-skia-runtime-opts", "--disable-lcd-text", "--font-render-hinting=none"])
            s = Shooter(browser, base)
            s.login_page("en")
            s.login_page("de")
            s.shares("en")
            s.folder("en")
            s.folder("de")
            s.scans("en")
            s.lightbox("en")
            s.lightbox("de")
            s.folder("en", dark=True, name="folder-dark-en")
            s.folder("en", phone=True, name="phone-folder-en")
            s.lightbox("en", phone=True, name="phone-lightbox-en")
            browser.close()
    finally:
        proc.terminate()
        proc.wait(timeout=20)


if __name__ == "__main__":
    main()
