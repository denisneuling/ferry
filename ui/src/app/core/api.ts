import { Injectable, inject } from '@angular/core';
import { Session } from './session';
import type { components } from './schema';

export type Info = components['schemas']['Info'];
export type Me = components['schemas']['Me'];
export type Share = components['schemas']['Share'];
export type Entry = components['schemas']['Entry'];
export type EntryList = components['schemas']['EntryList'];
export type Problem = components['schemas']['Problem'];

/** An answer of the API that is not a success: its status and stable code. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
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
    return this.json('GET', `${shareBase(share)}/entries?path=${q(path)}`);
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

  /** The URL a file is downloaded from, or shown inline from. */
  contentUrl(share: string, path: string, inline = false): string {
    return `${BASE}${shareBase(share)}/content?path=${q(path)}${inline ? '&disposition=inline' : ''}`;
  }

  thumbnailUrl(share: string, path: string, size: 160 | 320 | 640 = 320): string {
    return `${BASE}${shareBase(share)}/thumbnail?size=${size}&path=${q(path)}`;
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
    let code = 'internal';
    try {
      code = (JSON.parse(text) as Problem).code ?? code;
    } catch {
      // Not a problem response: a proxy in front, or the network.
    }
    const err = new ApiError(status, code);
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

/** Joins a folder and a name into an API path. */
export function join(dir: string, name: string): string {
  return dir ? `${dir}/${name}` : name;
}

/** The folder of an API path. */
export function parent(path: string): string {
  const i = path.lastIndexOf('/');
  return i < 0 ? '' : path.slice(0, i);
}
