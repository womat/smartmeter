"""Capture the README screenshots and the social preview of the smartmeter web UI.

Serves the real app/ui/index.html with a mocked /health (one source meter exporting about 3 kW,
the inverter on RS485 and one Modbus TCP client; polls and requests advance on every request, so
the LEDs flash and the rates are not zero) and photographs it with headless Chromium. Run from the
project root, on demand only:

    docker run --rm -v "$PWD":/src -w /src mcr.microsoft.com/playwright/python:v1.52.0-noble \
        sh -c 'pip install -q --break-system-packages playwright==1.52.0 && python3 docs/screenshots/capture.py'

Writes docs/screenshots/web-ui*.png and docs/social-preview.png.
"""

import base64
import json
import threading
import time
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parents[2]
PAGE = (ROOT / "app/ui/index.html").read_bytes()
OUT = ROOT / "docs/screenshots"
PORT = 8768
UPTIME = 3 * 86400 + 4 * 3600 + 12 * 60
START = time.time()

# Sample values of the source meter: export of 3141 W, balanced phases.
VALUES = {
    "power_total": -3141, "apparent_total": 3316, "reactive_total": -812, "pf_total": -0.95,
    "frequency": 50.01, "energy_import": 66_517_800, "energy_export": 16_983_400,
    "voltage_l1_l2": 404.9, "voltage_l2_l3": 405.2, "voltage_l3_l1": 404.1,
}
for n, (u, i, p, s, pf) in enumerate([(233.1, 5.12, -1146, 1193, -0.96), (234.6, 4.38, -958, 1027, -0.93),
                                      (232.8, 4.71, -1037, 1096, -0.95)], 1):
    VALUES.update({f"voltage_l{n}": u, f"current_l{n}": i, f"power_l{n}": p, f"apparent_l{n}": s, f"pf_l{n}": pf})


def iso(t):
    return datetime.fromtimestamp(t, timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")


def health():
    now = time.time()
    run = now - START
    return {
        "app": "smartmeter", "appVersion": "3.0.1", "goVersion": "go1.27.1", "hostname": "pi-meter", "os": "linux",
        "uptimeSeconds": UPTIME + run, "numGoroutines": 18, "heapAllocBytes": 2_900_000,
        "sysMemoryBytes": 11_800_000, "timestamp": iso(now),
        "meters": {"primary_meter": {
            "unitIds": [1, 200], "source": "tcp 192.168.1.10:502 unit 1", "ready": True, "online": True,
            "lastSuccess": iso(now - 0.4), "ageSeconds": 0.4, "latencyMs": 7.2,
            "polls": 270_000 + int(run), "discardedSnapshots": 0,
            "pollIntervalSeconds": 1, "staleTimeoutSeconds": 30, "maxCurrent": 63,
            "limits": {"importPower": 15000, "exportPower": 4500, "current": 20},
            "values": VALUES,
        }},
        "rtu": [{"port": "/dev/ttyS0", "connected": True, "since": iso(START - UPTIME)}],
        "modbus": {
            "tcp": {"address": "0.0.0.0:502", "requests": 260_000 + int(run), "clients": 1},
            "rtu": {"port": "/dev/ttyS0", "line": "9600 8N1", "requests": 2_400_000 + int(run * 9)},
        },
    }


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/":
            body, ctype = PAGE, "text/html; charset=utf-8"
        elif self.path == "/health":
            body, ctype = json.dumps(health()).encode(), "application/json"
        else:
            self.send_error(404)
            return
        self.send_response(200)
        self.send_header("Content-Type", ctype)
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


def shoot(browser, path, width, height, scheme, scale=1, full_page=True):
    ctx = browser.new_context(viewport={"width": width, "height": height},
                              color_scheme=scheme, device_scale_factor=scale)
    ctx.add_init_script("localStorage.setItem('smartmeter.apiKey', 'demo')")
    page = ctx.new_page()
    page.goto(f"http://localhost:{PORT}/")
    # The second poll brings the rates and the first flashes; catch a moment with a lit LED.
    page.wait_for_function("document.querySelector('.link .dot.on') !== null", polling="raf", timeout=10000)
    page.screenshot(path=str(path), full_page=full_page)
    ctx.close()
    print("wrote", path.relative_to(ROOT))


def social(browser):
    shot = base64.b64encode((OUT / "web-ui.png").read_bytes()).decode()
    html = f"""<!doctype html><meta charset="utf-8">
<style>
  body {{ margin: 0; width: 1280px; height: 640px; background: #f3f5f7; font-family: system-ui, sans-serif;
         display: flex; align-items: center; gap: 56px; padding: 0 0 0 96px; box-sizing: border-box; overflow: hidden; }}
  .text {{ display: flex; flex-direction: column; gap: 22px; width: 470px; flex: none; }}
  .brand {{ display: flex; align-items: center; gap: 22px; }}
  svg {{ width: 66px; height: 75px; color: #2563a8; flex: none; }}
  h1 {{ margin: 0; font-size: 64px; letter-spacing: -.02em; color: #17202b; }}
  h1 b {{ color: #2563a8; }}
  p {{ margin: 0; font-size: 30px; line-height: 1.3; color: #3d4a58; }}
  .tags {{ font-size: 21px; color: #5d6b7a; }}
  img {{ height: 520px; border-radius: 14px; box-shadow: 0 20px 50px rgba(23, 32, 43, .18);
         border: 1px solid #dde2e8; object-fit: cover; object-position: left top; width: 900px; }}
</style>
<div class="text">
  <div class="brand"><svg viewBox="0 0 30 34" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
    <rect x="3" y="2" width="24" height="30" rx="4"/><rect x="8" y="7" width="14" height="5" rx="1.2" stroke-width="1.8"/>
    <rect x="17.6" y="7.9" width="3.5" height="3.2" rx=".6" fill="#e8231a" stroke="none"/>
    <path d="M11.6 8.6v1.8M14.6 8.6v1.8" stroke="#5d6b7a" stroke-width="1"/>
    <rect x="8" y="18" width="14" height="7" rx="1.6" stroke-width="1.8"/><path d="M9.5 21.5h11" stroke="#5d6b7a" stroke-width="1.4"/>
    <rect x="14" y="19.2" width="2.4" height="4.6" fill="#e8231a" stroke="none"/></svg>
    <h1>smart<b>meter</b></h1></div>
  <p>Any Modbus energy meter as a Fronius Smart Meter 63A-3, on a Raspberry Pi.</p>
  <span class="tags">RS485 · SunSpec · live web page</span>
</div>
<img src="data:image/png;base64,{shot}" alt="">"""
    page = browser.new_page(viewport={"width": 1280, "height": 640})
    page.set_content(html)
    path = ROOT / "docs/social-preview.png"
    page.screenshot(path=str(path))
    page.close()
    print("wrote", path.relative_to(ROOT))


def main():
    server = ThreadingHTTPServer(("localhost", PORT), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    OUT.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch()
        shoot(browser, OUT / "web-ui.png", 1280, 900, "light")
        shoot(browser, OUT / "web-ui-dark.png", 1280, 900, "dark")
        # Phone: the first screen only.
        shoot(browser, OUT / "web-ui-phone.png", 390, 760, "light", scale=2, full_page=False)
        social(browser)
        browser.close()
    server.shutdown()


if __name__ == "__main__":
    main()
