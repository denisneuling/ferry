import { Injectable, inject } from '@angular/core';
import { Session } from './session';
import { Snapshot } from './snapshot';
import type { components } from './schema';

export type Info = components['schemas']['Info'];
export type Me = components['schemas']['Me'];
export type Share = components['schemas']['Share'];
export type Entry = components['schemas']['Entry'];
export type EntryList = components['schemas']['EntryList'];
export type ArchiveTicket = components['schemas']['ArchiveTicket'];
export type Space = components['schemas']['Space'];
export type FileMetadata = components['schemas']['FileMetadata'];
export type Problem = components['schemas']['Problem'];
export type Version = components['schemas']['Version'];
export type VersionList = components['schemas']['VersionList'];
export type SearchEnd = components['schemas']['SearchEnd']['end'];
export type Usage = components['schemas']['Usage'];
export type Sheet = components['schemas']['Sheet'];
export type Book = components['schemas']['Book'];
export type BookChapter = components['schemas']['BookChapter'];
export type BookNode = components['schemas']['BookNode'];
export type CopyProgress = components['schemas']['CopyProgress'];
export type ZipEntry = components['schemas']['ZipEntry'];

/** How a copy ended: the new entry, or the problem and how far it got. */
export interface CopyResult {
  entry?: Entry;
  error?: ApiError;
  progress: CopyProgress;
}

/** An answer of the API that is not a success: its status and stable code. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    /** For too_large: the bound that was reached, entries or bytes. */
    readonly limit?: number,
  ) {
    super(code);
  }
}

/** Progress of an upload, in bytes. */
export interface Progress {
  loaded: number;
  total: number;
}

const BASE = '/api/v1';

/**
 * ferry's API, as the interface uses it. Every request that changes something
 * carries X-Ferry-Request: 1, the CSRF guard the server checks. A 401 on
 * anything but the login ends the session here too: the server has ended it.
 */
@Injectable({ providedIn: 'root' })
export class Api {
  private readonly session = inject(Session);
  private readonly snapshot = inject(Snapshot);

  /**
   * The `at` parameter of a read: the given snapshot, or without one the
   * snapshot the interface browses (Snapshot); nothing for the share now.
   */
  private at(at?: string | null): string {
    const t = at === undefined ? this.snapshot.at() : at;
    return t ? `&at=${encodeURIComponent(t)}` : '';
  }

  info(): Promise<Info> {
    return this.json('GET', '/info');
  }

  login(username: string, password: string): Promise<Me> {
    return this.json('POST', '/login', { username, password });
  }

  async logout(): Promise<void> {
    await this.json('POST', '/logout');
  }

  me(): Promise<Me> {
    return this.json('GET', '/me');
  }

  async shares(): Promise<Share[]> {
    return (await this.json<{ shares: Share[] }>('GET', '/shares')).shares;
  }

  entries(share: string, path: string): Promise<EntryList> {
    return this.json('GET', `${shareBase(share)}/entries?path=${q(path)}${this.at()}`);
  }

  /** What the server's snapshots hold of a file or a folder, newest first. */
  versions(share: string, path: string): Promise<VersionList> {
    return this.json('GET', `${shareBase(share)}/versions?path=${q(path)}`);
  }

  /**
   * Lists a folder as the server reads it: onEntries gets the entries in
   * batches while they arrive, and the promise ends with their count. A
   * problem in the stream's last line, or a stream cut off before it, is an
   * ApiError like any other: a partial folder never passes for a whole one.
   */
  async entriesStream(
    share: string,
    path: string,
    onEntries: (batch: Entry[]) => void,
    signal?: AbortSignal,
  ): Promise<number> {
    const res = await this.fetch(
      'GET',
      `${BASE}${shareBase(share)}/entries?path=${q(path)}${this.at()}`,
      undefined,
      { Accept: 'application/x-ndjson' },
      signal,
    );
    if (!res.body) {
      throw new ApiError(0, 'network');
    }
    const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
    let rest = '';
    for (;;) {
      let chunk: ReadableStreamReadResult<string>;
      try {
        chunk = await reader.read();
      } catch {
        throw new ApiError(0, 'network');
      }
      if (chunk.done) {
        throw new ApiError(0, 'network'); // Cut off before the last line.
      }
      const lines = (rest + chunk.value).split('\n');
      rest = lines.pop() ?? '';
      const batch: Entry[] = [];
      for (const line of lines) {
        if (!line) continue;
        const v = JSON.parse(line) as Entry & { end?: { count: number }; problem?: Problem };
        if (v.end) {
          if (batch.length) onEntries(batch);
          void reader.cancel();
          return v.end.count;
        }
        if (v.problem) {
          void reader.cancel();
          throw this.problemOf(v.problem.status, v.problem);
        }
        batch.push(v);
      }
      if (batch.length) onEntries(batch);
    }
  }

  /**
   * Looks for names containing q below a folder: onHits gets the hits as
   * the server finds them, and the promise ends with how the search went.
   * A problem in the stream's last line, or a stream cut off before it, is
   * an ApiError, as for a listing.
   */
  /**
   * Adds up what a folder holds, the server counting folder by folder:
   * onProgress gets the count about once a second, the promise the last.
   */
  async usage(
    share: string,
    path: string,
    onProgress: (u: Usage) => void,
    signal?: AbortSignal,
  ): Promise<Usage> {
    const res = await this.fetch(
      'GET',
      `${BASE}${shareBase(share)}/usage?path=${encodeURIComponent(path)}${this.at()}`,
      undefined,
      { Accept: 'application/x-ndjson' },
      signal,
    );
    if (!res.body) throw new ApiError(0, 'network');
    const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
    let rest = '';
    for (;;) {
      let chunk: ReadableStreamReadResult<string>;
      try {
        chunk = await reader.read();
      } catch {
        throw new ApiError(0, 'network');
      }
      if (chunk.done) throw new ApiError(0, 'network');
      const lines = (rest + chunk.value).split('\n');
      rest = lines.pop() ?? '';
      for (const line of lines) {
        if (!line) continue;
        const v = JSON.parse(line) as { progress?: Usage; end?: Usage; problem?: Problem };
        if (v.progress) onProgress(v.progress);
        else if (v.end) {
          void reader.cancel();
          return v.end;
        } else if (v.problem) {
          void reader.cancel();
          throw this.problemOf(v.problem.status, v.problem);
        }
      }
    }
  }

  async search(
    share: string,
    path: string,
    q: string,
    onHits: (hits: Entry[]) => void,
    signal?: AbortSignal,
  ): Promise<SearchEnd> {
    const res = await this.fetch(
      'GET',
      `${BASE}${shareBase(share)}/search?path=${encodeURIComponent(path)}&q=${encodeURIComponent(q)}${this.at()}`,
      undefined,
      { Accept: 'application/x-ndjson' },
      signal,
    );
    if (!res.body) throw new ApiError(0, 'network');
    const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
    let rest = '';
    for (;;) {
      let chunk: ReadableStreamReadResult<string>;
      try {
        chunk = await reader.read();
      } catch {
        throw new ApiError(0, 'network');
      }
      if (chunk.done) throw new ApiError(0, 'network');
      const lines = (rest + chunk.value).split('\n');
      rest = lines.pop() ?? '';
      const hits: Entry[] = [];
      for (const line of lines) {
        if (!line) continue;
        const v = JSON.parse(line) as { hit?: Entry; end?: SearchEnd; problem?: Problem };
        if (v.hit) hits.push(v.hit);
        else if (v.end) {
          if (hits.length) onHits(hits);
          void reader.cancel();
          return v.end;
        } else if (v.problem) {
          void reader.cancel();
          throw this.problemOf(v.problem.status, v.problem);
        }
      }
      if (hits.length) onHits(hits);
    }
  }

  /**
   * Copies a file or a folder within a share, from a snapshot with at: a
   * version restored as a copy. What the server refuses before it begins
   * is thrown; once it runs, onProgress hears about every second, and the
   * result says how it ended — a copy that failed part-way keeps what it
   * copied, and says how much.
   */
  async copy(
    share: string,
    from: string,
    to: string,
    opts: { overwrite?: boolean; at?: string | null },
    onProgress: (p: CopyProgress) => void,
    signal?: AbortSignal,
  ): Promise<CopyResult> {
    const body: Record<string, unknown> = { from, to };
    if (opts.overwrite) body['overwrite'] = true;
    if (opts.at) body['at'] = opts.at;
    return this.copyStream(`${shareBase(share)}/copy`, body, onProgress, signal);
  }

  /** Unpacks a ZIP archive into a new folder on the share; its stream is a copy's. */
  unpack(
    share: string,
    from: string,
    to: string,
    onProgress: (p: CopyProgress) => void,
  ): Promise<CopyResult> {
    const body: Record<string, unknown> = { from, to };
    const at = this.snapshot.at();
    if (at) body['at'] = at;
    return this.copyStream(`${shareBase(share)}/unpack`, body, onProgress);
  }

  /** Packs entries of one folder into a ZIP file on the share; its stream is a copy's. */
  saveArchive(
    share: string,
    paths: string[],
    to: string,
    onProgress: (p: CopyProgress) => void,
  ): Promise<CopyResult> {
    return this.copyStream(`${shareBase(share)}/archive-to-share`, { paths, to }, onProgress);
  }

  /**
   * A request answered as a copy is: refused before it begins, or progress
   * about every second and how it ended.
   */
  private async copyStream(
    path: string,
    body: Record<string, unknown>,
    onProgress: (p: CopyProgress) => void,
    signal?: AbortSignal,
  ): Promise<CopyResult> {
    const res = await this.fetch('POST', `${BASE}${path}`, body, { Accept: 'application/x-ndjson' }, signal);
    if (!res.body) throw new ApiError(0, 'network');
    const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
    let rest = '';
    let last: CopyProgress = { files: 0, bytes: 0, totalFiles: 0, totalBytes: 0 };
    for (;;) {
      let chunk: ReadableStreamReadResult<string>;
      try {
        chunk = await reader.read();
      } catch {
        return { error: new ApiError(0, 'network'), progress: last };
      }
      if (chunk.done) return { error: new ApiError(0, 'network'), progress: last };
      const lines = (rest + chunk.value).split('\n');
      rest = lines.pop() ?? '';
      for (const line of lines) {
        if (!line) continue;
        const v = JSON.parse(line) as {
          progress?: CopyProgress;
          end?: { entry: Entry; progress: CopyProgress };
          problem?: Problem;
        };
        if (v.end) {
          void reader.cancel();
          return { entry: v.end.entry, progress: v.end.progress };
        }
        if (v.progress) last = v.progress;
        if (v.problem) {
          void reader.cancel();
          return { error: this.problemOf(v.problem.status, v.problem), progress: last };
        }
        if (v.progress) onProgress(v.progress);
      }
    }
  }

  /** What a ZIP archive holds, as its table of contents says. */
  async zip(share: string, path: string): Promise<ZipEntry[]> {
    return (
      await this.json<{ entries: ZipEntry[] }>('GET', `${shareBase(share)}/zip?path=${q(path)}${this.at()}`)
    ).entries;
  }

  /** The URL one entry of a ZIP is downloaded from, or shown inline from. */
  zipEntryUrl(share: string, path: string, entry: string, inline = false): string {
    return `${BASE}${shareBase(share)}/zip/entry?path=${q(path)}&entry=${encodeURIComponent(entry)}${inline ? '&disposition=inline' : ''}${this.at()}`;
  }

  /** The plain text of a Word or OpenDocument text, read by the server. */
  documentText(share: string, path: string): Promise<{ text: string; truncated: boolean }> {
    return this.json('GET', `${shareBase(share)}/text?path=${q(path)}${this.at()}`);
  }

  /** The first sheet of a workbook, its cells as text, read by the server. */
  sheet(share: string, path: string, index = 0): Promise<Sheet> {
    return this.json(
      'GET',
      `${shareBase(share)}/sheet?path=${q(path)}${index ? `&sheet=${index}` : ''}${this.at()}`,
    );
  }

  /** An EPUB's title and chapters, read by the server. */
  book(share: string, path: string): Promise<Book> {
    return this.json('GET', `${shareBase(share)}/book?path=${q(path)}${this.at()}`);
  }

  /** One chapter of an EPUB, as a tree of text. */
  bookChapter(share: string, path: string, entry: string): Promise<BookChapter> {
    return this.json(
      'GET',
      `${shareBase(share)}/book/chapter?path=${q(path)}&entry=${encodeURIComponent(entry)}${this.at()}`,
    );
  }

  /** The size and free space of the volume a folder is on, as the server reports them. */
  space(share: string, path: string): Promise<Space> {
    return this.json('GET', `${shareBase(share)}/space?path=${q(path)}`);
  }

  /**
   * What a file says about itself, read by the server from its bytes. With
   * the entry's version the browser keeps the answer until the file changes.
   */
  metadata(share: string, path: string, version?: string, at?: string | null): Promise<FileMetadata> {
    return this.json('GET', `${shareBase(share)}/metadata?path=${q(path)}${v(version)}${this.at(at)}`);
  }

  mkdir(share: string, path: string): Promise<Entry> {
    return this.json('POST', `${shareBase(share)}/folders?path=${q(path)}`);
  }

  move(share: string, from: string, to: string, overwrite = false): Promise<Entry> {
    return this.json('POST', `${shareBase(share)}/move`, { from, to, overwrite });
  }

  async remove(share: string, path: string, recursive = false): Promise<void> {
    await this.json(
      'DELETE',
      `${shareBase(share)}/entries?path=${q(path)}${recursive ? '&recursive=true' : ''}`,
    );
  }

  /**
   * Lists entries of one folder for a ZIP; the answer's ticket is good for
   * one download, by this session, within a minute.
   */
  archive(share: string, paths: string[]): Promise<ArchiveTicket> {
    return this.json('POST', `${shareBase(share)}/archive`, { paths });
  }

  /** The URL a listed ZIP downloads from. */
  archiveUrl(share: string, ticket: string): string {
    return `${BASE}${shareBase(share)}/archive/${encodeURIComponent(ticket)}`;
  }

  /** The URL a file is downloaded from, or shown inline from. */
  contentUrl(share: string, path: string, inline = false, at?: string | null): string {
    return `${BASE}${shareBase(share)}/content?path=${q(path)}${inline ? '&disposition=inline' : ''}${this.at(at)}`;
  }

  /**
   * A thumbnail's URL. With the entry's version the browser keeps the
   * thumbnail until the file changes, and coming back to a folder costs no
   * request at all.
   */
  thumbnailUrl(
    share: string,
    path: string,
    size: 160 | 320 | 640 = 320,
    version?: string,
    at?: string | null,
  ): string {
    return `${BASE}${shareBase(share)}/thumbnail?size=${size}&path=${q(path)}${v(version)}${this.at(at)}`;
  }

  /** The start of a text file, at most max bytes, as text. */
  async text(share: string, path: string, max = 512 * 1024): Promise<{ text: string; truncated: boolean }> {
    const r = await this.head(share, path, max);
    return { text: new TextDecoder().decode(r.bytes), truncated: r.truncated };
  }

  /**
   * The first max bytes of a file, at the snapshot looked at, and whether
   * there was more.
   */
  async head(share: string, path: string, max: number): Promise<{ bytes: Uint8Array; truncated: boolean }> {
    const res = await this.fetch('GET', this.contentUrl(share, path, true), undefined, {
      Range: `bytes=0-${max - 1}`,
    });
    const buf = await res.arrayBuffer();
    const total = Number(res.headers.get('Content-Range')?.split('/')[1] ?? buf.byteLength);
    return { bytes: new Uint8Array(buf), truncated: total > buf.byteLength };
  }

  /**
   * A file's bytes, as it is now, and the ETag naming that version: what an
   * editor opens. More than max bytes is too_large.
   */
  async readFile(share: string, path: string, max: number): Promise<{ bytes: Uint8Array; etag: string }> {
    const res = await this.fetch(
      'GET',
      `${BASE}${shareBase(share)}/content?path=${q(path)}`,
      undefined,
      {},
      undefined,
    );
    const size = Number(res.headers.get('Content-Length') ?? 0);
    if (size > max) {
      void res.body?.cancel();
      throw new ApiError(413, 'too_large', max);
    }
    const bytes = new Uint8Array(await res.arrayBuffer());
    if (bytes.length > max) throw new ApiError(413, 'too_large', max);
    return { bytes, etag: res.headers.get('ETag') ?? '' };
  }

  /**
   * Saves an editor's bytes over the version etag names; a file that
   * changed since is a 412 (changed), and stays as it is. Without etag the
   * file is replaced whatever it is now: the person chose to.
   */
  async saveFile(share: string, path: string, bytes: Uint8Array, etag: string | null): Promise<Entry> {
    const headers: Record<string, string> = {
      'X-Ferry-Request': '1',
      'Content-Type': 'application/octet-stream',
    };
    if (etag) headers['If-Match'] = etag;
    let res: Response;
    try {
      res = await fetch(
        `${BASE}${shareBase(share)}/content?path=${q(path)}${etag ? '' : '&overwrite=true'}`,
        {
          method: 'PUT',
          headers,
          body: new Blob([bytes as Uint8Array<ArrayBuffer>]),
          credentials: 'same-origin',
        },
      );
    } catch {
      throw new ApiError(0, 'network');
    }
    if (!res.ok) throw this.problem(res.status, await res.text().catch(() => ''));
    return (await res.json()) as Entry;
  }

  /**
   * Uploads a file with progress, which fetch cannot report. abort() cancels
   * it; the server then leaves nothing behind.
   */
  upload(
    share: string,
    path: string,
    file: Blob,
    overwrite: boolean,
    progress: (p: Progress) => void,
  ): { done: Promise<Entry>; abort: () => void } {
    const xhr = new XMLHttpRequest();
    const done = new Promise<Entry>((resolve, reject) => {
      xhr.open(
        'PUT',
        `${BASE}${shareBase(share)}/content?path=${q(path)}${overwrite ? '&overwrite=true' : ''}`,
      );
      xhr.setRequestHeader('X-Ferry-Request', '1');
      xhr.setRequestHeader('Content-Type', 'application/octet-stream');
      xhr.upload.onprogress = (e) =>
        progress({ loaded: e.loaded, total: e.lengthComputable ? e.total : file.size });
      xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) {
          resolve(JSON.parse(xhr.responseText) as Entry);
          return;
        }
        reject(this.problem(xhr.status, xhr.responseText));
      };
      xhr.onerror = () => reject(new ApiError(0, 'network'));
      xhr.onabort = () => reject(new ApiError(0, 'aborted'));
      xhr.send(file);
    });
    return { done, abort: () => xhr.abort() };
  }

  /**
   * A large file uploaded in pieces (the server's uploads in pieces): one
   * piece after the other, each retried after a broken connection from
   * where the server says the upload has come to. Where an upload stands is
   * kept in this browser under the target and the file's name, size and
   * time, so that the same file uploaded to the same place again — after a
   * reload — goes on where it stopped. Aborted, it is given up and its
   * partial file removed.
   */
  uploadInPieces(
    share: string,
    path: string,
    file: File,
    overwrite: boolean,
    progress: (p: Progress) => void,
  ): { done: Promise<Entry>; abort: () => void } {
    let aborted = false;
    let xhr: XMLHttpRequest | null = null;
    let id: string | null = null;
    const key = resumeKey(share, path, file);
    const base = `${BASE}${shareBase(share)}/uploads`;
    const at = (i: string) => `${base}/${i}?path=${q(path)}`;
    const piece = (offset: number, blob: Blob) =>
      new Promise<number>((resolve, reject) => {
        const x = new XMLHttpRequest();
        xhr = x;
        x.open('PUT', `${at(id!)}&offset=${offset}`);
        x.setRequestHeader('X-Ferry-Request', '1');
        x.setRequestHeader('Content-Type', 'application/octet-stream');
        x.upload.onprogress = (e) => progress({ loaded: offset + e.loaded, total: file.size });
        x.onload = () =>
          x.status === 200
            ? resolve((JSON.parse(x.responseText) as { size: number }).size)
            : reject(this.problem(x.status, x.responseText));
        x.onerror = () => reject(new ApiError(0, 'network'));
        x.onabort = () => reject(new ApiError(0, 'aborted'));
        x.send(blob);
      });
    const done = (async () => {
      id = readResume(key);
      let offset = 0;
      if (id) {
        try {
          offset = (await this.json<{ size: number }>('GET', at(id).slice(BASE.length))).size;
        } catch {
          id = null; // gone, or not ours to go on with: begin anew
        }
      }
      if (!id || offset > file.size) {
        const begun = await this.json<{ id: string }>(
          'POST',
          `${shareBase(share)}/uploads?path=${q(path)}${overwrite ? '&overwrite=true' : ''}`,
        );
        id = begun.id;
        offset = 0;
        writeResume(key, id);
      }
      let tries = 0;
      while (offset < file.size) {
        if (aborted) throw new ApiError(0, 'aborted');
        try {
          offset = await piece(offset, file.slice(offset, offset + PIECE));
          tries = 0;
        } catch (e) {
          if (aborted || !(e instanceof ApiError)) throw e;
          // Broken, out of step, or another tab's piece of the same upload
          // being written: wait, ask where it stands, go on.
          const again =
            e.status === 0 ||
            e.code === 'upload_offset' ||
            e.code === 'busy' ||
            e.status === 502 ||
            e.status === 503;
          if (!again || ++tries > RETRIES) throw e;
          // Wait a moment, then ask where the upload has come to: the piece
          // may have arrived in part, or whole with its answer lost.
          await new Promise((r) => setTimeout(r, 1000 * 2 ** (tries - 1)));
          offset = (await this.json<{ size: number }>('GET', at(id).slice(BASE.length))).size;
        }
      }
      const e = await this.json<Entry>(
        'POST',
        `${shareBase(share)}/uploads/${id}/finish?path=${q(path)}&size=${file.size}${overwrite ? '&overwrite=true' : ''}`,
      );
      forgetResume(key);
      return e;
    })();
    return {
      done,
      abort: () => {
        aborted = true;
        (xhr as XMLHttpRequest | null)?.abort();
        forgetResume(key);
        const i = id;
        if (i) void this.json('DELETE', at(i).slice(BASE.length)).catch(() => undefined);
      },
    };
  }

  private async json<T>(method: string, path: string, body?: unknown): Promise<T> {
    const res = await this.fetch(method, BASE + path, body);
    if (res.status === 204) {
      return undefined as T;
    }
    return (await res.json()) as T;
  }

  private async fetch(
    method: string,
    url: string,
    body?: unknown,
    headers: Record<string, string> = {},
    signal?: AbortSignal,
  ): Promise<Response> {
    const h: Record<string, string> = { ...headers };
    if (method !== 'GET') {
      h['X-Ferry-Request'] = '1';
    }
    if (body !== undefined) {
      h['Content-Type'] = 'application/json';
    }
    let res: Response;
    try {
      res = await fetch(url, {
        method,
        headers: h,
        body: body === undefined ? undefined : JSON.stringify(body),
        credentials: 'same-origin',
        signal,
      });
    } catch {
      throw new ApiError(0, 'network');
    }
    if (!res.ok) {
      throw this.problem(res.status, await res.text().catch(() => ''));
    }
    return res;
  }

  private problem(status: number, text: string): ApiError {
    let p: Partial<Problem> = {};
    try {
      p = JSON.parse(text) as Problem;
    } catch {
      // Not a problem response: a proxy in front, or the network.
    }
    return this.problemOf(status, p);
  }

  private problemOf(status: number, p: Partial<Problem>): ApiError {
    const code = p.code ?? 'internal';
    const err = new ApiError(status, code, typeof p.limit === 'number' ? p.limit : undefined);
    if (status === 401 && code === 'unauthenticated') {
      this.session.ended();
    }
    if (code === 'session_lost') {
      this.session.ended('lost');
    }
    return err;
  }
}

function shareBase(share: string): string {
  return `/shares/${encodeURIComponent(share)}`;
}

function q(path: string): string {
  return encodeURIComponent(path);
}

/** The `v` parameter for an entry's version, or nothing without one. */
function v(version: string | undefined): string {
  return version ? `&v=${encodeURIComponent(version)}` : '';
}

/** Joins a folder and a name into an API path. */
export function join(dir: string, name: string): string {
  return dir ? `${dir}/${name}` : name;
}

/** The folder of an API path. */
export function parent(path: string): string {
  const i = path.lastIndexOf('/');
  return i < 0 ? '' : path.slice(0, i);
}

/** Files from this size on are uploaded in pieces, and can go on after a break. */
export const PIECES_FROM = 16 * 1024 * 1024;
/** One piece: large enough to keep the line busy, small enough to lose little. */
const PIECE = 8 * 1024 * 1024;
/** Times a piece is tried again after a broken connection, waiting 1, 2, 4… seconds. */
const RETRIES = 6;
/** Uploads remembered for going on; the oldest go first. */
const MAX_RESUMES = 50;
const RESUME_KEY = 'ferry.uploads';

function resumeKey(share: string, path: string, f: File): string {
  return [share, path, f.name, f.size, f.lastModified].join('\u0000');
}

function resumes(): [string, string][] {
  try {
    const v = JSON.parse(localStorage.getItem(RESUME_KEY) ?? '[]') as unknown;
    return Array.isArray(v)
      ? v.filter(
          (x): x is [string, string] =>
            Array.isArray(x) &&
            typeof x[0] === 'string' &&
            typeof x[1] === 'string' &&
            /^[0-9a-f]{32}$/.test(x[1]),
        )
      : [];
  } catch {
    return [];
  }
}

function saveResumes(list: [string, string][]): void {
  try {
    localStorage.setItem(RESUME_KEY, JSON.stringify(list.slice(-MAX_RESUMES)));
  } catch {
    // Storage blocked: an upload goes on after a break, not after a reload.
  }
}

function readResume(key: string): string | null {
  return resumes().find(([k]) => k === key)?.[1] ?? null;
}

function writeResume(key: string, id: string): void {
  saveResumes([...resumes().filter(([k]) => k !== key), [key, id]]);
}

function forgetResume(key: string): void {
  saveResumes(resumes().filter(([k]) => k !== key));
}
