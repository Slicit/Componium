/* Where every lane sits.
 *
 * One track can occupy one row or several: a colour curve expanded is three
 * channel lanes, collapsed it is a single ribbon. The header column and the
 * canvas have to agree about that down to the pixel, and the cheapest way to
 * guarantee it is to compute it once, here, and have both read the answer.
 *
 * No DOM, so it is testable — and worth testing, because "the labels are one
 * row out from the lanes" is the kind of defect that looks like a rendering
 * glitch and is actually two pieces of arithmetic disagreeing.
 */

import { channelsOf, isHSI, type Rig, type Track } from './score';

export const ROW_CUE = 54;
export const ROW_CHANNEL = 34;
export const ROW_COLLAPSED = 34;

export interface Row {
  /** Index into score.tracks. */
  track: number;
  instrument: string;
  /** A channel lane names its channel; a summary row does not. */
  channel?: string;
  /** True for the row that carries the instrument's name and controls. */
  head: boolean;
  /** How this row should be drawn. */
  draw: 'cues' | 'curve' | 'ribbon' | 'envelope';
  /**
   * False for the compound row of a multi-channel track.
   *
   * That row shows what the channels add up to — the colour a light actually
   * makes — which is a reading of three values rather than a value in its own
   * right. There is nothing there to drag: moving "the colour" up has no
   * single meaning. So it is drawn and never edited, and the channels beneath
   * it are where the editing happens.
   */
  editable: boolean;
  y: number;
  h: number;
}

export interface Layout {
  rows: Row[];
  height: number;
}

export interface LayoutOptions {
  collapsed: Set<string>;
  rig?: Rig | null;
  /** Instruments to leave out entirely. */
  hidden?: Set<string>;
  /** Display order, by instrument id. Anything unlisted keeps score order. */
  order?: string[];
}

/**
 * Order tracks for display.
 *
 * The score's own order is whatever the composer emitted and is not
 * meaningful; a person's arrangement is. Unlisted instruments keep their
 * relative position at the end rather than being dropped, so a rebuild that
 * adds a track cannot make it invisible.
 */
export function orderTracks(tracks: Track[], order?: string[]): number[] {
  const index = tracks.map((_, i) => i);
  if (!order?.length) return index;
  const rank = new Map(order.map((id, i) => [id, i]));
  return index.sort((a, b) => {
    const ra = rank.get(tracks[a].instrument);
    const rb = rank.get(tracks[b].instrument);
    if (ra === undefined && rb === undefined) return a - b;
    if (ra === undefined) return 1;
    if (rb === undefined) return -1;
    return ra - rb;
  });
}

export function layout(tracks: Track[], opts: LayoutOptions): Layout {
  const rows: Row[] = [];
  let y = 0;

  for (const ti of orderTracks(tracks, opts.order)) {
    const track = tracks[ti];
    if (opts.hidden?.has(track.instrument)) continue;

    if (track.type !== 'curve') {
      rows.push({
        track: ti, instrument: track.instrument, head: true,
        draw: 'cues', editable: true, y, h: ROW_CUE,
      });
      y += ROW_CUE;
      continue;
    }

    const channels = channelsOf(track, opts.rig);

    /* A single-channel curve has nothing to collapse *into*: its one lane is
     * already the whole story, and offering to fold it would be a control that
     * changes the row height and nothing else. */
    if (channels.length < 2) {
      rows.push({
        track: ti, instrument: track.instrument, channel: channels[0],
        head: true, draw: 'curve', editable: true, y, h: ROW_CHANNEL,
      });
      y += ROW_CHANNEL;
      continue;
    }

    /* A colour track's compound view is the colour it actually makes, which
     * says more about a look than three value graphs do. Everything else
     * compounds to its amplitude envelope. */
    const summary = isColourTrack(track, channels) ? 'ribbon' as const : 'envelope' as const;

    rows.push({
      track: ti, instrument: track.instrument, head: true,
      draw: summary, editable: false, y, h: ROW_COLLAPSED,
    });
    y += ROW_COLLAPSED;

    /* Expanded keeps that compound row and adds the channels beneath it, so
     * the first lane is never secretly red: the row carrying the instrument's
     * name shows the instrument, and every channel gets a row of its own with
     * its own label. */
    if (opts.collapsed.has(track.instrument)) continue;

    for (const channel of channels) {
      rows.push({
        track: ti, instrument: track.instrument, channel,
        head: false, draw: 'curve', editable: true, y, h: ROW_CHANNEL,
      });
      y += ROW_CHANNEL;
    }
  }

  return { rows, height: y };
}

/**
 * Whether folding this track means anything.
 *
 * Only multi-channel curves. Everything else already shows all it has, and a
 * chevron on it would be a control that does nothing but change a height.
 */
export function canCollapse(track: Track, rig?: Rig | null): boolean {
  return track.type === 'curve' && channelsOf(track, rig).length > 1;
}

/** What the compound row of a track is called. */
export function summaryLabel(track: Track, rig?: Rig | null): string {
  return isColourTrack(track, channelsOf(track, rig)) ? 'colour' : 'all';
}

/** A track whose channels together describe one colour. */
function isColourTrack(track: Track, channels: string[]): boolean {
  if (isHSI(track)) return true;
  return channels.length > 1 && channels.every((c) => 'rgb'.includes(c));
}

/** Which row a y coordinate is in, or null above or below everything. */
export function rowAt(l: Layout, y: number): Row | null {
  for (const r of l.rows) {
    if (y >= r.y && y < r.y + r.h) return r;
  }
  return null;
}

/** Every row belonging to one instrument, for hit-testing a group. */
export function rowsOf(l: Layout, instrument: string): Row[] {
  return l.rows.filter((r) => r.instrument === instrument);
}
