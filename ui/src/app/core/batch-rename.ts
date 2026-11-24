import { Entry } from './api';
import { checkName } from '../ui/dialog';

/**
 * Renaming several entries by a pattern: the new names worked out and
 * checked before anything is renamed. A name the server could not take, two
 * entries meant to get the same name, or a name another entry of the folder
 * has, and nothing is renamed at all.
 *
 * The pattern's parts: {name} — the old name without its extension; {ext}
 * — the extension, without the dot; {n} or {n:3} — a counter, padded to
 * that many digits; {date} — the day it was last modified; {taken} — the
 * day a photo was taken, its modification day where the camera wrote none.
 */

export interface RenameOptions {
  /** The counter's first value. */
  start: number;
  /** Digits the counter is padded to, where the pattern says none. */
  digits: number;
}

export interface RenameRow {
  entry: Entry;
  to: string;
  /** Why the name cannot be: the whole rename is off while one has one. */
  problem: 'invalid' | 'twice' | 'taken' | null;
}

export interface RenamePlan {
  rows: RenameRow[];
  /** Whether every new name may be: then, and only then, it may run. */
  ok: boolean;
  /** How many entries get another name. */
  changes: number;
  /** Whether some entry takes a name another of them gives up: a swap, done through temporary names. */
  swaps: boolean;
}

/** How names compare on an SMB share: case aside, and after NFC. */
export function nameKey(name: string): string {
  return name.normalize('NFC').toLowerCase();
}

function split(e: Entry): { stem: string; ext: string } {
  if (e.type === 'folder') return { stem: e.name, ext: '' };
  const i = e.name.lastIndexOf('.');
  return i > 0 ? { stem: e.name.slice(0, i), ext: e.name.slice(i + 1) } : { stem: e.name, ext: '' };
}

/** A day as YYYY-MM-DD; a camera's time as its clock showed it, a modification in the viewer's zone. */
function day(iso: string, local: boolean): string {
  if (!local) return iso.slice(0, 10);
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso.slice(0, 10);
  const p = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

/** One entry's new name by the pattern. */
export function applyPattern(
  pattern: string,
  e: Entry,
  index: number,
  opts: RenameOptions,
  taken?: string,
): string {
  const { stem, ext } = split(e);
  const n = opts.start + index;
  const out = pattern.replace(/\{(name|ext|date|taken|n)(?::(\d{1,2}))?\}/g, (_, key: string, w?: string) => {
    switch (key) {
      case 'name':
        return stem;
      case 'ext':
        return ext;
      case 'date':
        return day(e.modified, true);
      case 'taken':
        return taken ? day(taken, false) : day(e.modified, true);
      default:
        return String(n).padStart(w ? Number(w) : opts.digits, '0');
    }
  });
  return out.trim();
}

/**
 * The new names of the entries chosen, in their order, checked against one
 * another and against the folder's other entries (all of them, the chosen
 * included).
 */
export function planRename(
  chosen: Entry[],
  folder: Entry[],
  pattern: string,
  opts: RenameOptions,
  taken: ReadonlyMap<string, string> = new Map(),
): RenamePlan {
  const rows: RenameRow[] = chosen.map((entry, i) => ({
    entry,
    to: applyPattern(pattern, entry, i, opts, taken.get(entry.name)),
    problem: null,
  }));
  const mine = new Set(chosen.map((e) => nameKey(e.name)));
  // Names the folder keeps: those of entries not renamed.
  const kept = new Set(folder.filter((e) => !mine.has(nameKey(e.name))).map((e) => nameKey(e.name)));
  const count = new Map<string, number>();
  for (const r of rows) count.set(nameKey(r.to), (count.get(nameKey(r.to)) ?? 0) + 1);
  for (const r of rows) {
    if (checkName(r.to) !== null) r.problem = 'invalid';
    else if ((count.get(nameKey(r.to)) ?? 0) > 1) r.problem = 'twice';
    else if (kept.has(nameKey(r.to))) r.problem = 'taken';
  }
  const changing = rows.filter((r) => r.to !== r.entry.name);
  // A swap: an entry is to take a name another of them still has.
  const given = new Map(changing.map((r) => [nameKey(r.entry.name), r]));
  const swaps = changing.some((r) => {
    const holder = given.get(nameKey(r.to));
    return holder !== undefined && holder !== r;
  });
  return {
    rows,
    ok: rows.every((r) => r.problem === null) && changing.length > 0,
    changes: changing.length,
    swaps,
  };
}

/** The steps a plan takes: straight, or through a temporary name each when names are swapped. */
export interface RenameStep {
  from: string;
  to: string;
  /** The step parks the entry under a temporary name. */
  temp: boolean;
  /** The entry's name before the rename began. */
  original: string;
}

export function steps(plan: RenamePlan, tempName: (i: number) => string): RenameStep[] {
  const changing = plan.rows.filter((r) => r.to !== r.entry.name);
  if (!plan.swaps) {
    // A change of case only, or a name no one else had: one step each.
    return changing.map((r) => ({ from: r.entry.name, to: r.to, temp: false, original: r.entry.name }));
  }
  const temps = changing.map((_, i) => tempName(i));
  return [
    ...changing.map((r, i) => ({ from: r.entry.name, to: temps[i], temp: true, original: r.entry.name })),
    ...changing.map((r, i) => ({ from: temps[i], to: r.to, temp: false, original: r.entry.name })),
  ];
}
