#!/usr/bin/env python3
"""Generate the Mizan application icon.

# Why this is a script and not a binary asset

`build/appicon.png` is the input to every platform's icon pipeline, and a 1024x1024 PNG
committed as an opaque blob is an asset nobody can adjust in five years. The geometry below is
the source: change a number, re-run, and every size regenerates.

It also keeps the offline constraint. The machine has no ImageMagick, no Pillow, no
rsvg-convert — so this renders PNG directly with nothing but `zlib` from the standard library,
and leans on macOS's own `sips`/`iconutil` for resampling and the .icns container.

# The mark

Mizan (ميزان) is Arabic for *balance* — a scale. The name is the mark: a beam, a column, two
pans. It says "financial precision" without a currency symbol that would tie the product to a
country it deliberately does not assume (Addendum §C), and the pans read as containers, which
is the inventory half of the product.

Drawn with signed distance fields and analytic anti-aliasing rather than supersampling: exact
edges at 1024 with no 16x memory blow-up, in pure Python.

Usage:
    python3 scripts/icon.py              # build/appicon.png + build/icon.svg
    python3 scripts/icon.py --ico DIR    # pack DIR/*.png into build/windows/icon.ico
"""

from __future__ import annotations

import math
import struct
import zlib
from pathlib import Path

SIZE = 1024
ROOT = Path(__file__).resolve().parent.parent
BUILD = ROOT / "build"

# ── palette ─────────────────────────────────────────────────────────────────────
#
# The gradient runs from the product's own primary (--color-primary, blue-600) into blue-950,
# so the icon and the application it launches are visibly the same thing. An icon in colours
# the app never uses is a small, permanent inconsistency.
GRADIENT_FROM = (37, 99, 235)
GRADIENT_TO = (23, 37, 84)
MARK = (255, 255, 255)

# ── geometry, in a 1024 square ──────────────────────────────────────────────────
#
# The single source of truth for both the raster and the vector output below, so the two
# cannot drift.
CORNER = 224.0

BEAM = dict(cx=512, cy=330, hx=336, hy=26, r=26)
COLUMN = dict(cx=512, cy=545, hx=28, hy=235, r=28)
BASE = dict(cx=512, cy=792, hx=176, hy=28, r=28)
PIVOT = dict(cx=512, cy=268, r=46)
# The pans hang directly under the beam ends, with no cords.
#
# A first draft drew them: two short bars from the beam down to each pan. They were wrong in a
# way that only showed up on screen — a cord ends at the middle of a bowl, which is its OPENING,
# not its rim, so each one dangled into empty space. Attaching to the rim instead would put two
# thin diagonals into an icon that has to survive being 16 pixels wide.
#
# Dropping them entirely reads better at every size: the eye completes the connection, and the
# mark is three shapes instead of five.
PANS = [dict(cx=272, cy=430, r=132, w=52), dict(cx=752, cy=430, r=132, w=52)]


# ── signed distance fields ──────────────────────────────────────────────────────
#
# Each returns the distance from a point to the shape's edge: negative inside, positive
# outside, in pixels. Coverage is then `clamp(0.5 - d, 0, 1)`, which is a one-pixel-wide linear
# ramp across the boundary — the cheapest anti-aliasing that is actually correct.


def sd_rounded_box(x: float, y: float, cx: float, cy: float, hx: float, hy: float, r: float) -> float:
    qx = abs(x - cx) - (hx - r)
    qy = abs(y - cy) - (hy - r)
    outside = math.hypot(max(qx, 0.0), max(qy, 0.0))
    return outside + min(max(qx, qy), 0.0) - r


def sd_circle(x: float, y: float, cx: float, cy: float, r: float) -> float:
    return math.hypot(x - cx, y - cy) - r


def sd_lower_ring(x: float, y: float, cx: float, cy: float, r: float, w: float) -> float:
    """A pan: the bottom half of an annulus.

    Intersecting two fields is `max`, so clipping the ring to y >= cy is one more term.
    """
    ring = abs(math.hypot(x - cx, y - cy) - r) - w / 2.0
    return max(ring, cy - y)


def mark_distance(x: float, y: float) -> float:
    """Distance to the whole mark — the union of every part, which is `min`."""
    d = sd_rounded_box(x, y, **BEAM)
    d = min(d, sd_rounded_box(x, y, **COLUMN))
    d = min(d, sd_rounded_box(x, y, **BASE))
    d = min(d, sd_circle(x, y, **PIVOT))
    for pan in PANS:
        d = min(d, sd_lower_ring(x, y, **pan))
    return d


def coverage(d: float) -> float:
    return min(max(0.5 - d, 0.0), 1.0)


# ── PNG ─────────────────────────────────────────────────────────────────────────


def write_png(path: Path, width: int, height: int, rgba: bytearray) -> None:
    """Write an 8-bit RGBA PNG.

    Hand-rolled because the machine has no imaging library and the format's minimum viable
    subset is three chunks. Filter type 0 on every row: the image is a smooth gradient, so a
    cleverer filter would buy a smaller file and nothing else.
    """
    raw = bytearray()
    stride = width * 4
    for row in range(height):
        raw.append(0)
        raw += rgba[row * stride : (row + 1) * stride]

    def chunk(tag: bytes, data: bytes) -> bytes:
        return (
            struct.pack(">I", len(data))
            + tag
            + data
            + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)
        )

    header = struct.pack(">IIBBBBB", width, height, 8, 6, 0, 0, 0)
    path.write_bytes(
        b"\x89PNG\r\n\x1a\n"
        + chunk(b"IHDR", header)
        + chunk(b"IDAT", zlib.compress(bytes(raw), 9))
        + chunk(b"IEND", b"")
    )


def render(size: int) -> bytearray:
    scale = size / SIZE
    pixels = bytearray(size * size * 4)
    gr0, gg0, gb0 = GRADIENT_FROM
    gr1, gg1, gb1 = GRADIENT_TO
    mr, mg, mb = MARK

    for py in range(size):
        y = (py + 0.5) / scale
        row = py * size * 4
        for px in range(size):
            x = (px + 0.5) / scale

            # The plate. Distances are computed in the 1024 space, so they are scaled back to
            # device pixels before the coverage ramp — otherwise the anti-aliased edge would be
            # 64 pixels wide on a 16px icon.
            plate = sd_rounded_box(x, y, SIZE / 2, SIZE / 2, SIZE / 2, SIZE / 2, CORNER) * scale
            alpha = coverage(plate)
            if alpha <= 0.0:
                continue

            # A diagonal gradient: top-left is the product's primary, bottom-right sinks into
            # near-black so the white mark stays legible over the whole plate.
            t = (x + y) / (2 * SIZE)
            r = gr0 + (gr1 - gr0) * t
            g = gg0 + (gg1 - gg0) * t
            b = gb0 + (gb1 - gb0) * t

            ink = coverage(mark_distance(x, y) * scale)
            if ink > 0.0:
                r += (mr - r) * ink
                g += (mg - g) * ink
                b += (mb - b) * ink

            i = row + px * 4
            pixels[i] = int(r + 0.5)
            pixels[i + 1] = int(g + 0.5)
            pixels[i + 2] = int(b + 0.5)
            pixels[i + 3] = int(alpha * 255 + 0.5)
    return pixels


# ── SVG ─────────────────────────────────────────────────────────────────────────


def write_svg(path: Path) -> None:
    """Emit the same geometry as a vector, for anyone who wants to edit it in a real tool.

    Generated from the constants above rather than maintained by hand, so it cannot disagree
    with what the raster pipeline actually produces.
    """

    def box(d: dict) -> str:
        return (
            f'<rect x="{d["cx"] - d["hx"]}" y="{d["cy"] - d["hy"]}" '
            f'width="{d["hx"] * 2}" height="{d["hy"] * 2}" rx="{d["r"]}" fill="#fff"/>'
        )

    def pan(d: dict) -> str:
        cx, cy, r, w = d["cx"], d["cy"], d["r"], d["w"]
        return (
            f'<path d="M {cx - r} {cy} A {r} {r} 0 0 0 {cx + r} {cy}" '
            f'fill="none" stroke="#fff" stroke-width="{w}" stroke-linecap="round"/>'
        )

    parts = [
        f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {SIZE} {SIZE}" width="{SIZE}" height="{SIZE}">',
        "<!-- Generated by scripts/icon.py. Edit the geometry there, not here. -->",
        "<defs><linearGradient id='g' x1='0' y1='0' x2='1' y2='1'>"
        f"<stop offset='0' stop-color='rgb{GRADIENT_FROM}'/>"
        f"<stop offset='1' stop-color='rgb{GRADIENT_TO}'/>"
        "</linearGradient></defs>",
        f'<rect width="{SIZE}" height="{SIZE}" rx="{CORNER}" fill="url(#g)"/>',
        box(BEAM),
        box(COLUMN),
        box(BASE),
        f'<circle cx="{PIVOT["cx"]}" cy="{PIVOT["cy"]}" r="{PIVOT["r"]}" fill="#fff"/>',
        *[pan(p) for p in PANS],
        "</svg>",
    ]
    path.write_text("\n".join(parts) + "\n")


# ── ICO ─────────────────────────────────────────────────────────────────────────


def write_ico(pngs: list[Path], out: Path) -> None:
    """Pack PNGs into a Windows .ico.

    ICO has accepted embedded PNG since Vista, which makes the container trivial: a header, one
    16-byte directory entry per image, then the PNG bytes verbatim. No BMP encoding, no AND
    mask, no bottom-up rows.

    Written here rather than shelled out because macOS ships nothing that produces .ico, and the
    alternative — committing a binary somebody would have to regenerate on another machine —
    is the thing this whole script exists to avoid.
    """
    images = []
    for path in sorted(pngs, key=lambda p: int(p.stem)):
        size = int(path.stem)
        if size > 256:
            continue  # the format's dimension field is one byte
        images.append((size, path.read_bytes()))

    header = struct.pack("<HHH", 0, 1, len(images))
    offset = len(header) + 16 * len(images)
    directory, blobs = b"", b""
    for size, data in images:
        # 0 means 256 in a one-byte dimension — the format's own convention.
        dim = 0 if size == 256 else size
        directory += struct.pack("<BBBBHHII", dim, dim, 0, 0, 1, 32, len(data), offset)
        blobs += data
        offset += len(data)

    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_bytes(header + directory + blobs)
    print(f"wrote {out} ({len(images)} sizes)")


def main() -> None:
    import sys

    if len(sys.argv) > 2 and sys.argv[1] == "--ico":
        source = Path(sys.argv[2])
        write_ico(list(source.glob("*.png")), BUILD / "windows" / "icon.ico")
        return

    BUILD.mkdir(parents=True, exist_ok=True)
    write_png(BUILD / "appicon.png", SIZE, SIZE, render(SIZE))
    write_svg(BUILD / "icon.svg")
    print(f"wrote {BUILD / 'appicon.png'} and {BUILD / 'icon.svg'}")


if __name__ == "__main__":
    main()
