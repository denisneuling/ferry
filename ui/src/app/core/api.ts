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
    const res = await this.fetch('GET', this.contentUrl(share, path, true), undefined, {
      Range: `bytes=0-${max - 1}`,
    });
    const buf = await res.arrayBuffer();
    const total = Number(res.headers.get('Content-Range')?.split('/')[1] ?? buf.byteLength);
    return { text: new TextDecoder().decode(buf), truncated: total > buf.byteLength };
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
