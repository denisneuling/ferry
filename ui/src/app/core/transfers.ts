import { Injectable, computed, inject, signal } from '@angular/core';
import { Api, ApiError, join } from './api';
import { Browser } from './browser';
import { Dialogs } from '../ui/dialog';
import { Toasts } from '../ui/toast';
import { describe } from './errors';

export type JobState = 'queued' | 'running' | 'done' | 'failed' | 'skipped';

export interface Job {
  id: number;
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
      added.push({ id, name: f.name, share, dir, where, size: f.size, loaded: 0, state: 'queued' });
    }
    if (!added.length) {
      return;
    }
    this.jobs.update((j) => [...j, ...added]);
    this.open.set(true);
    this.pump();
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
