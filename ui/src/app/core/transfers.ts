import { Injectable, computed, inject, signal } from '@angular/core';
import { Api, ApiError, join, parent } from './api';
import { Browser } from './browser';
import { Dialogs } from '../ui/dialog';
import { Toasts } from '../ui/toast';
import { describe } from './errors';

export type JobState = 'queued' | 'running' | 'done' | 'failed' | 'skipped';

export interface Job {
  id: number;
  /** An upload from the browser, or a copy the server makes. */
  kind: 'upload' | 'copy';
  name: string;
  share: string;
  dir: string;
  /** What the folder is called in the panel. */
  where: string;
  size: number;
  loaded: number;
  state: JobState;
  error?: string;
}

/** Uploads running at once: enough to keep the line busy, few enough to finish one soon. */
const PARALLEL = 2;

/** The most files one folder upload takes; the server's ZIP bound, by default. */
export const MAX_TREE_FILES = 10000;

/** A file of a folder being uploaded, with its path below that folder. */
export interface TreeFile {
  rel: string;
  file: File;
}

/** What a copy running on the server reports to the panel. */
export interface CopyJob {
  progress(loaded: number): void;
  finish(error?: string): void;
}

/**
 * The uploads: a queue that runs a few at a time, shows progress, asks before
 * replacing a file, and reloads the folder each one lands in.
 */
@Injectable({ providedIn: 'root' })
export class Transfers {
  private readonly api = inject(Api);
  private readonly browser = inject(Browser);
  private readonly dialogs = inject(Dialogs);
  private readonly toasts = inject(Toasts);

  readonly jobs = signal<Job[]>([]);
  readonly open = signal(true);
  readonly running = computed(
    () => this.jobs().filter((j) => j.state === 'queued' || j.state === 'running').length,
  );

  private files = new Map<number, File>();
  private aborts = new Map<number, () => void>();
  private next = 1;
  private active = 0;
  /** What the current batch did, for the note when it is finished. */
  private batchDone = 0;
  private batchFailed = 0;

  add(share: string, dir: string, where: string, files: Iterable<File>): void {
    const added: Job[] = [];
    for (const f of files) {
      const id = this.next++;
      this.files.set(id, f);
      added.push({
        id,
        kind: 'upload',
        name: f.name,
        share,
        dir,
        where,
        size: f.size,
        loaded: 0,
        state: 'queued',
      });
    }
    if (!added.length) {
      return;
    }
    this.jobs.update((j) => [...j, ...added]);
    this.open.set(true);
    this.pump();
  }

  /**
   * Uploads a folder: its folders made first, in order, then its files
   * queued into them. More files than max is refused, with nothing made.
   * A folder that is there already is used as it is.
   */
  async addTree(share: string, dir: string, items: TreeFile[], max = MAX_TREE_FILES): Promise<void> {
    if (!items.length) return;
    if (items.length > max) {
      this.toasts.error(
        $localize`:@@upload.treeTooMany:${items.length}:n: files are more than one upload takes (${max}:max:). Upload the folder in parts.`,
      );
      return;
    }
    const folders = new Set<string>();
    for (const it of items) {
      for (let d = parent(it.rel); d; d = parent(d)) folders.add(d);
    }
    // Parents before their children: by depth, then by name.
    const ordered = [...folders].sort(
      (a, b) => a.split('/').length - b.split('/').length || a.localeCompare(b),
    );
    for (const f of ordered) {
      try {
        await this.api.mkdir(share, join(dir, f));
      } catch (e) {
        if (e instanceof ApiError && e.code === 'exists') continue;
        this.toasts.error(`${f}: ${describe(e, 'upload')}`);
        return;
      }
    }
    if (this.browser.share() === share && this.browser.path() === dir) void this.browser.reload();
    const byFolder = new Map<string, File[]>();
    for (const it of items) {
      const d = parent(it.rel);
      byFolder.set(d, [...(byFolder.get(d) ?? []), it.file]);
    }
    for (const [d, files] of byFolder) {
      const where = d ? d.slice(d.lastIndexOf('/') + 1) : this.browser.title();
      this.add(share, join(dir, d), where, files);
    }
  }

  /** A copy the server makes, shown in the panel with its progress. */
  trackCopy(share: string, dir: string, name: string, where: string, size: number): CopyJob {
    const id = this.next++;
    this.jobs.update((j) => [
      ...j,
      { id, kind: 'copy', name, share, dir, where, size, loaded: 0, state: 'running' },
    ]);
    this.open.set(true);
    return {
      progress: (loaded) => this.set(id, { loaded }),
      finish: (error) => this.set(id, error ? { state: 'failed', error } : { state: 'done', loaded: size }),
    };
  }

  /** Removes the finished jobs from the panel. */
  clearFinished(): void {
    this.jobs.update((j) => j.filter((x) => x.state === 'queued' || x.state === 'running'));
  }

  /** Stops every upload: on logout. The server removes what was half written. */
  cancelAll(): void {
    this.aborts.forEach((a) => a());
    this.jobs.set([]);
    this.files.clear();
  }

  private set(id: number, patch: Partial<Job>): void {
    this.jobs.update((j) => j.map((x) => (x.id === id ? { ...x, ...patch } : x)));
  }

  private pump(): void {
    while (this.active < PARALLEL) {
      const job = this.jobs().find((j) => j.state === 'queued');
      if (!job) {
        return;
      }
      this.active++;
      this.set(job.id, { state: 'running' });
      this.run(job, false).finally(() => {
        this.active--;
        this.files.delete(job.id);
        this.aborts.delete(job.id);
        this.pump();
        if (!this.running()) {
          this.finished();
        }
      });
    }
  }

  private async run(job: Job, overwrite: boolean): Promise<void> {
    const file = this.files.get(job.id);
    if (!file) {
      return;
    }
    const up = this.api.upload(job.share, join(job.dir, job.name), file, overwrite, (p) =>
      this.set(job.id, { loaded: p.loaded }),
    );
    this.aborts.set(job.id, up.abort);
    try {
      await up.done;
      this.set(job.id, { state: 'done', loaded: job.size });
      this.batchDone++;
      if (this.browser.share() === job.share && this.browser.path() === job.dir) {
        void this.browser.reload();
      }
    } catch (e) {
      if (e instanceof ApiError && e.code === 'exists' && !overwrite) {
        const replace = await this.dialogs.confirm({
          title: $localize`:@@upload.replaceTitle:Replace “${job.name}:name:”?`,
          text: $localize`:@@upload.replaceText:A file with this name is already in “${job.where}:where:”. Replacing it overwrites it.`,
          ok: $localize`:@@upload.replace:Replace`,
          danger: true,
        });
        if (replace) {
          this.set(job.id, { loaded: 0 });
          return this.run(job, true);
        }
        this.set(job.id, { state: 'skipped' });
        return;
      }
      if (e instanceof ApiError && e.code === 'aborted') {
        return;
      }
      this.set(job.id, { state: 'failed', error: describe(e, 'upload') });
      this.batchFailed++;
    }
  }

  private finished(): void {
    const done = this.batchDone;
    const failed = this.batchFailed;
    this.batchDone = 0;
    this.batchFailed = 0;
    if (failed) {
      this.toasts.error(
        failed === 1
          ? $localize`:@@upload.failed1:One upload failed.`
          : $localize`:@@upload.failedN:${failed}:n: uploads failed.`,
      );
    } else if (done) {
      this.toasts.show(
        done === 1
          ? $localize`:@@upload.done1:1 file uploaded.`
          : $localize`:@@upload.doneN:${done}:n: files uploaded.`,
      );
    }
  }
}
