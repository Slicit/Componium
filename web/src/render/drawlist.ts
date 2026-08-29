/* A drawing, as data.
 *
 * The renderer does not touch a canvas. It turns a score and a view into a
 * list of primitives, and something else executes that list against a real
 * context. That split exists for one reason: the browser this project is
 * developed against cannot be relied on to run anything, and a canvas that
 * draws nothing looks exactly like one that works. A draw list can be asserted
 * on in node — "at this zoom, this cue is a rect at x=… this tall, in this
 * colour" — which is the only way the drawing itself gets tested at all.
 *
 * It is also how the performance budget becomes measurable: the length of the
 * list is the work, and it can be counted without painting.
 */

export type Align = 'left' | 'center' | 'right';

export interface Rect {
  k: 'rect';
  x: number; y: number; w: number; h: number;
  fill?: string;
  stroke?: string;
  lineWidth?: number;
  radius?: number;
  /** Diagonal hatching, for anything that is a warning rather than a value. */
  hatch?: boolean;
  alpha?: number;
}

export interface Line {
  k: 'line';
  x1: number; y1: number; x2: number; y2: number;
  stroke: string;
  lineWidth?: number;
  dash?: number[];
  alpha?: number;
}

/** A polyline, and its filled area when `fill` is set. */
export interface Path {
  k: 'path';
  pts: number[];
  stroke?: string;
  fill?: string;
  lineWidth?: number;
  /** Close down to this y before filling, for an area under a curve. */
  baseline?: number;
  alpha?: number;
}

export interface Dot {
  k: 'dot';
  x: number; y: number; r: number;
  fill?: string;
  stroke?: string;
  lineWidth?: number;
}

export interface Text {
  k: 'text';
  x: number; y: number;
  s: string;
  fill: string;
  align?: Align;
  size?: number;
  weight?: number;
  mono?: boolean;
  alpha?: number;
}

/** A horizontal gradient strip: the collapsed colour track. */
export interface Ribbon {
  k: 'ribbon';
  x: number; y: number; w: number; h: number;
  stops: Array<{ at: number; colour: string }>;
}

export type Prim = Rect | Line | Path | Dot | Text | Ribbon;

/** A draw list, with the small amount of bookkeeping a caller wants back. */
export class DrawList {
  readonly items: Prim[] = [];
  /** Counted so a test can assert the renderer skipped what is off screen. */
  culled = 0;

  push(p: Prim): this {
    this.items.push(p);
    return this;
  }

  rect(r: Omit<Rect, 'k'>): this { return this.push({ k: 'rect', ...r }); }
  line(l: Omit<Line, 'k'>): this { return this.push({ k: 'line', ...l }); }
  path(p: Omit<Path, 'k'>): this { return this.push({ k: 'path', ...p }); }
  dot(d: Omit<Dot, 'k'>): this { return this.push({ k: 'dot', ...d }); }
  text(t: Omit<Text, 'k'>): this { return this.push({ k: 'text', ...t }); }
  ribbon(r: Omit<Ribbon, 'k'>): this { return this.push({ k: 'ribbon', ...r }); }

  get length(): number { return this.items.length; }

  of<K extends Prim['k']>(kind: K): Extract<Prim, { k: K }>[] {
    return this.items.filter((p) => p.k === kind) as Extract<Prim, { k: K }>[];
  }
}

/** Execute a draw list against a 2D context. The only part that needs a canvas. */
export function paint(ctx: CanvasRenderingContext2D, list: DrawList, fontStack: string, monoStack: string): void {
  for (const p of list.items) {
    ctx.save();
    if ('alpha' in p && typeof p.alpha === 'number') ctx.globalAlpha = p.alpha;

    switch (p.k) {
      case 'rect': {
        if (p.radius) {
          ctx.beginPath();
          ctx.roundRect(p.x, p.y, p.w, p.h, p.radius);
        } else {
          ctx.beginPath();
          ctx.rect(p.x, p.y, p.w, p.h);
        }
        if (p.fill) { ctx.fillStyle = p.fill; ctx.fill(); }
        if (p.hatch) {
          ctx.save();
          ctx.clip();
          ctx.strokeStyle = p.stroke ?? p.fill ?? '#000';
          ctx.lineWidth = 1;
          for (let x = p.x - p.h; x < p.x + p.w; x += 6) {
            ctx.beginPath();
            ctx.moveTo(x, p.y + p.h);
            ctx.lineTo(x + p.h, p.y);
            ctx.stroke();
          }
          ctx.restore();
        }
        if (p.stroke && !p.hatch) {
          ctx.strokeStyle = p.stroke;
          ctx.lineWidth = p.lineWidth ?? 1;
          ctx.stroke();
        }
        break;
      }
      case 'line': {
        ctx.beginPath();
        ctx.moveTo(p.x1, p.y1);
        ctx.lineTo(p.x2, p.y2);
        ctx.strokeStyle = p.stroke;
        ctx.lineWidth = p.lineWidth ?? 1;
        if (p.dash) ctx.setLineDash(p.dash);
        ctx.stroke();
        break;
      }
      case 'path': {
        if (p.pts.length >= 4) {
          ctx.beginPath();
          ctx.moveTo(p.pts[0], p.pts[1]);
          for (let i = 2; i < p.pts.length; i += 2) ctx.lineTo(p.pts[i], p.pts[i + 1]);
          if (p.fill && typeof p.baseline === 'number') {
            ctx.save();
            ctx.lineTo(p.pts[p.pts.length - 2], p.baseline);
            ctx.lineTo(p.pts[0], p.baseline);
            ctx.closePath();
            ctx.fillStyle = p.fill;
            ctx.fill();
            ctx.restore();
            /* Re-trace, because filling consumed the closed path and the
             * stroke must not include the two baseline legs. */
            ctx.beginPath();
            ctx.moveTo(p.pts[0], p.pts[1]);
            for (let i = 2; i < p.pts.length; i += 2) ctx.lineTo(p.pts[i], p.pts[i + 1]);
          }
          if (p.stroke) {
            ctx.strokeStyle = p.stroke;
            ctx.lineWidth = p.lineWidth ?? 1.5;
            ctx.lineJoin = 'round';
            ctx.stroke();
          }
        }
        break;
      }
      case 'dot': {
        ctx.beginPath();
        ctx.arc(p.x, p.y, p.r, 0, Math.PI * 2);
        if (p.fill) { ctx.fillStyle = p.fill; ctx.fill(); }
        if (p.stroke) {
          ctx.strokeStyle = p.stroke;
          ctx.lineWidth = p.lineWidth ?? 1.4;
          ctx.stroke();
        }
        break;
      }
      case 'text': {
        ctx.fillStyle = p.fill;
        ctx.textAlign = p.align ?? 'left';
        ctx.textBaseline = 'alphabetic';
        ctx.font = `${p.weight ?? 400} ${p.size ?? 11}px ${p.mono ? monoStack : fontStack}`;
        ctx.fillText(p.s, p.x, p.y);
        break;
      }
      case 'ribbon': {
        const g = ctx.createLinearGradient(p.x, 0, p.x + p.w, 0);
        for (const s of p.stops) g.addColorStop(Math.max(0, Math.min(1, s.at)), s.colour);
        ctx.fillStyle = g;
        ctx.fillRect(p.x, p.y, p.w, p.h);
        break;
      }
    }
    ctx.restore();
  }
}
