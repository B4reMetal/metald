// Visualizer styles. Each draws one frame from 12 band levels (0..1, already smoothed) onto a canvas
// sized w x h in CSS pixels. They only differ in drawing; the levels come from the server either way.

import type { Palette } from "./themes";

export type Draw = (g: CanvasRenderingContext2D, levels: number[], w: number, h: number, p: Palette, t: number) => void;

export interface Visualizer { id: string; label: string; draw: Draw }

function gradient(g: CanvasRenderingContext2D, p: Palette, x0: number, y0: number, x1: number, y1: number) {
  const gr = g.createLinearGradient(x0, y0, x1, y1);
  gr.addColorStop(0, p.accent);
  gr.addColorStop(1, p.accent2);
  return gr;
}

const bars: Draw = (g, lv, w, h, p) => {
  const n = lv.length, gap = 6, bw = (w - gap * (n + 1)) / n, top = 12, floor = h - 12;
  g.fillStyle = p.accent;
  lv.forEach((v, i) => {
    const bh = Math.max(3, v * (floor - top));
    g.globalAlpha = 0.35 + 0.65 * v;
    g.beginPath();
    g.roundRect(gap + i * (bw + gap), floor - bh, bw, bh, Math.min(4, bw / 2));
    g.fill();
  });
  g.globalAlpha = 1;
};

const mirror: Draw = (g, lv, w, h, p) => {
  // Bass in the middle, treble to both edges, bars growing up and down from the centre line.
  const order = [...lv].reverse().concat(lv);
  const n = order.length, gap = 3, bw = (w - gap * (n + 1)) / n, mid = h / 2;
  g.fillStyle = gradient(g, p, 0, 0, 0, h);
  order.forEach((v, i) => {
    const half = Math.max(1.5, v * (mid - 10));
    g.beginPath();
    g.roundRect(gap + i * (bw + gap), mid - half, bw, half * 2, Math.min(3, bw / 2));
    g.fill();
  });
};

const wave: Draw = (g, lv, w, h, p, t) => {
  // A smooth curve through the bands, drawn twice: a soft fill and a bright line.
  const floor = h - 10, pts = lv.map((v, i) => [(i / (lv.length - 1)) * w, floor - v * (h - 30) - Math.sin(t * 2 + i) * 2 * v]);
  const path = () => {
    g.beginPath();
    g.moveTo(0, pts[0][1]);
    for (let i = 1; i < pts.length; i++) {
      const [x0, y0] = pts[i - 1], [x1, y1] = pts[i], cx = (x0 + x1) / 2;
      g.bezierCurveTo(cx, y0, cx, y1, x1, y1);
    }
  };
  path();
  g.lineTo(w, h);
  g.lineTo(0, h);
  g.closePath();
  g.fillStyle = gradient(g, p, 0, 0, w, 0);
  g.globalAlpha = 0.25;
  g.fill();
  g.globalAlpha = 1;
  path();
  g.strokeStyle = gradient(g, p, 0, 0, w, 0);
  g.lineWidth = 3;
  g.lineCap = "round";
  g.stroke();
};

const radial: Draw = (g, lv, w, h, p, t) => {
  // Bands as spokes around a pulsing ring, slowly turning.
  const cx = w / 2, cy = h / 2, r = Math.min(w, h) * 0.18, reach = Math.min(w, h) * 0.3;
  const spokes = lv.concat(lv).concat(lv), n = spokes.length, avg = lv.reduce((a, b) => a + b, 0) / lv.length;
  g.strokeStyle = gradient(g, p, cx - reach, cy - reach, cx + reach, cy + reach);
  g.lineCap = "round";
  g.lineWidth = Math.max(2, (2 * Math.PI * r) / n - 3);
  spokes.forEach((v, i) => {
    const a = (i / n) * Math.PI * 2 + t * 0.2;
    g.beginPath();
    g.moveTo(cx + Math.cos(a) * r, cy + Math.sin(a) * r);
    g.lineTo(cx + Math.cos(a) * (r + 3 + v * reach), cy + Math.sin(a) * (r + 3 + v * reach));
    g.stroke();
  });
  g.beginPath();
  g.arc(cx, cy, r * (0.75 + avg * 0.2), 0, Math.PI * 2);
  g.fillStyle = p.accent;
  g.globalAlpha = 0.15 + avg * 0.35;
  g.fill();
  g.globalAlpha = 1;
};

const led: Draw = (g, lv, w, h, p) => {
  // A hi-fi meter: columns of segments, the top few in the second colour.
  const n = lv.length, rows = 14, gapX = 6, gapY = 3;
  const cw = (w - gapX * (n + 1)) / n, ch = (h - 20 - gapY * (rows - 1)) / rows;
  lv.forEach((v, i) => {
    const lit = Math.round(v * rows);
    for (let r = 0; r < rows; r++) {
      g.fillStyle = r >= rows - 3 ? p.accent2 : p.accent;
      g.globalAlpha = r < lit ? 0.95 : 0.1;
      g.fillRect(gapX + i * (cw + gapX), h - 10 - (r + 1) * ch - r * gapY, cw, ch);
    }
  });
  g.globalAlpha = 1;
};

export const VISUALIZERS: Visualizer[] = [
  { id: "bars", label: "Bars", draw: bars },
  { id: "mirror", label: "Mirror", draw: mirror },
  { id: "wave", label: "Wave", draw: wave },
  { id: "radial", label: "Radial", draw: radial },
  { id: "led", label: "LED meter", draw: led },
];

export function visualizerById(id: string): Visualizer {
  return VISUALIZERS.find((v) => v.id === id) ?? VISUALIZERS[0];
}
