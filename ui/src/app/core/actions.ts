import { Injectable, inject } from '@angular/core';
import { Api, CopyProgress, CopyResult, Entry, join, parent } from './api';
import { Browser } from './browser';
import { describe } from './errors';
import { Format } from './format';
import { Session } from './session';
import { Dialogs, checkName } from '../ui/dialog';
import { Toasts } from '../ui/toast';
import { FolderPicker } from '../ui/folder-picker';
import { CopyJob, Transfers } from './transfers';

/**
 * What can be done to entries of the open folder. Each action asks what it
 * must, tells what happened, and reloads the folder; what the server refuses
 * is said in everyday words (errors.ts).
 */
@Injectable({ providedIn: 'root' })
export class Actions {
  private readonly api = inject(Api);
  private readonly browser = inject(Browser);
  private readonly dialogs = inject(Dialogs);
  private readonly toasts = inject(Toasts);
  private readonly picker = inject(FolderPicker);
  private readonly transfers = inject(Transfers);
  private readonly session = inject(Session);
  private readonly format = inject(Format);

  /**
   * Downloads what is chosen: one file as it is, through a plain link with
   * the server's attachment header; anything more — several entries, or a
   * folder — as one ZIP, which the server lists first (so that what it
   * refuses is said here, before anything downloads) and then streams.
   */
  async download(entries: Entry[]): Promise<void> {
    const share = this.browser.share();
    if (!share || !entries.length) return;
    if (entries.length === 1 && entries[0].type === 'file') {
      this.save(this.api.contentUrl(share, entries[0].path), entries[0].name);
      this.toasts.show($localize`:@@download.one:Downloading ${entries[0].name}:name:`);
      return;
    }
    // FERRY_MAX_ARCHIVE bounds what one ZIP holds, folders' contents
    // counted by the server; the selection itself is held to it here.
    const max = this.session.info()?.limits?.archive;
    if (max && entries.length > max) {
      this.toasts.error(
        $localize`:@@zip.tooMany:${this.format.count(entries.length)}:n: items are more than ferry puts into one ZIP (${this.format.count(max)}:max:). Select fewer.`,
      );
      return;
    }
    try {
      const t = await this.api.archive(
        share,
        entries.map((e) => e.path),
      );
      this.save(this.api.archiveUrl(share, t.ticket), t.name);
      this.toasts.show(
        $localize`:@@zip.started:Downloading ${t.name}:name: — ${this.format.count(t.entries)}:n: items, ${this.format.size(t.bytes)}:size:`,
      );
    } catch (e) {
      this.toasts.error(describe(e, 'archive'));
    }
  }

  /** Lets the browser download a URL, as a click on a link would. */
  private save(url: string, name: string): void {
    const a = document.createElement('a');
    a.href = url;
    a.download = name;
    a.rel = 'noopener';
    document.body.appendChild(a);
    a.click();
    a.remove();
  }

  async mkdir(): Promise<void> {
    const share = this.browser.share();
    if (share === null) return;
    const name = await this.dialogs.ask({
      title: $localize`:@@mkdir.title:New folder`,
      field: $localize`:@@mkdir.field:Name`,
      value: $localize`:@@mkdir.value:New folder`,
      ok: $localize`:@@mkdir.ok:Create`,
      check: checkName,
    });
    if (!name) return;
    try {
      await this.api.mkdir(share, join(this.browser.path(), name));
      await this.browser.reload();
      this.browser.only(name);
      this.toasts.show($localize`:@@mkdir.done:Folder “${name}:name:” created.`);
    } catch (e) {
      this.toasts.error(describe(e));
    }
  }

  /**
   * A new, empty text file, opened in the editor. An existing file of that
   * name is never replaced: the server answers exists, and that is said.
   */
  async newText(): Promise<void> {
    const share = this.browser.share();
    if (share === null) return;
    const name = await this.dialogs.ask({
      title: $localize`:@@newText.title:New text file`,
      field: $localize`:@@mkdir.field:Name`,
      value: $localize`:@@newText.value:Notes.txt`,
      selectStem: true,
      ok: $localize`:@@mkdir.ok:Create`,
      check: checkName,
    });
    if (!name) return;
    try {
      await this.api.upload(share, join(this.browser.path(), name), new Blob([]), false, () => undefined)
        .done;
      await this.browser.reload();
      const i = this.browser.files().findIndex((e) => e.name === name);
      if (i >= 0) {
        this.browser.editNext = true;
        this.browser.preview.set(i);
      }
    } catch (e) {
      this.toasts.error(describe(e));
    }
  }

  /** Unpacks a ZIP archive into a new folder beside it, named as the person says. */
  async unpack(entry: Entry): Promise<void> {
    const share = this.browser.share();
    if (share === null) return;
    const dir = this.browser.path();
    const stem = entry.name.replace(/\.zip$/i, '');
    const name = await this.dialogs.ask({
      title: $localize`:@@unpack.title:Unpack “${entry.name}:name:”`,
      field: $localize`:@@unpack.field:Into the new folder`,
      value: stem,
      ok: $localize`:@@unpack.ok:Unpack`,
      check: checkName,
    });
    if (!name) return;
    const to = join(dir, name);
    const stays = $localize`:@@unpack.stays:The folder “${name}:name:” stays with what was unpacked.`;
    if (
      await this.copyOne(share, to, entry.name, (on) => this.api.unpack(share, entry.path, to, on), stays)
    ) {
      if (share === this.browser.share() && dir === this.browser.path()) await this.browser.reload();
    }
  }

  /** Packs entries into a ZIP file on the share, beside them. */
  async zipToShare(entries: Entry[]): Promise<void> {
    const share = this.browser.share();
    if (share === null || !entries.length) return;
    const dir = this.browser.path();
    const base =
      entries.length === 1
        ? entries[0].name.replace(/\.[^.]*$/, '') || entries[0].name
        : this.browser.title();
    const name = await this.dialogs.ask({
      title: $localize`:@@zipShare.title:Save as a ZIP file here`,
      field: $localize`:@@zipShare.field:Name`,
      value: `${base}.zip`,
      selectStem: true,
      ok: $localize`:@@zipShare.ok:Save`,
      check: checkName,
    });
    if (!name) return;
    const to = join(dir, name);
    const paths = entries.map((e) => e.path);
    if (await this.copyOne(share, to, name, (on) => this.api.saveArchive(share, paths, to, on))) {
      if (share === this.browser.share() && dir === this.browser.path()) await this.browser.reload();
    }
  }

  async rename(entry: Entry): Promise<void> {
    const share = this.browser.share();
    if (share === null) return;
    const to = await this.dialogs.ask({
      title: $localize`:@@rename.title:Rename`,
      field: $localize`:@@rename.field:New name`,
      value: entry.name,
      selectStem: entry.type === 'file',
      ok: $localize`:@@rename.ok:Rename`,
      check: checkName,
    });
    if (!to || to === entry.name) return;
    try {
      await this.api.move(share, entry.path, join(this.browser.path(), to));
      await this.browser.reload();
      this.browser.only(to);
    } catch (e) {
      this.toasts.error(describe(e));
    }
  }

  async move(entries: Entry[]): Promise<void> {
    const share = this.browser.share();
    if (share === null || !entries.length) return;
    const dest = await this.picker.pick(
      share,
      this.browser.path(),
      entries.filter((e) => e.type === 'folder').map((e) => e.path),
      entries.length,
    );
    if (dest === null) return;
    await this.moveTo(entries, dest);
  }

  /** Moves entries into a folder: what the picker chose, or where they were dropped. */
  async moveTo(entries: Entry[], dest: string): Promise<void> {
    const share = this.browser.share();
    if (share === null || !entries.length || dest === this.browser.path()) return;
    let moved = 0;
    const failures: string[] = [];
    for (const e of entries) {
      try {
        await this.api.move(share, e.path, join(dest, e.name));
        moved++;
      } catch (err) {
        failures.push(`${e.name}: ${describe(err)}`);
      }
    }
    this.browser.clear();
    await this.browser.reload();
    if (failures.length) {
      this.toasts.error(
        failures.length === 1
          ? failures[0]
          : $localize`:@@move.someFailed:${failures.length}:n: items could not be moved.`,
      );
    }
    if (moved) {
      this.toasts.show(
        moved === 1 ? $localize`:@@move.one:Moved.` : $localize`:@@move.many:${moved}:n: items moved.`,
      );
    }
  }

  /**
   * Copies entries into a folder the person picks; the server copies, and
   * says how far it got. A copy that fails part-way keeps what it copied.
   */
  async copy(entries: Entry[]): Promise<void> {
    const share = this.browser.share();
    if (share === null || !entries.length) return;
    const dest = await this.picker.pick(
      share,
      this.browser.path(),
      entries.filter((e) => e.type === 'folder').map((e) => e.path),
      entries.length,
      true,
    );
    if (dest === null) return;
    for (const e of entries) {
      const to = await this.freeName(
        share,
        dest,
        e.name,
        dest === this.browser.path() ? $localize`:@@copy.suffix:copy` : null,
      );
      await this.copyOne(share, to, e.name, (on) => this.api.copy(share, e.path, to, {}, on));
    }
    await this.browser.reload();
  }

  /**
   * A previous version restored as a copy beside the original, named after
   * the snapshot's time as the person's clock shows it: "letter (version
   * 23.11.2026 16.00).txt".
   */
  async restore(share: string, path: string, at: string, when: string): Promise<void> {
    const dir = parent(path);
    const name = path.slice(path.lastIndexOf('/') + 1);
    const to = await this.freeName(share, dir, name, $localize`:@@restore.suffix:version ${when}:when:`);
    if (await this.copyOne(share, to, name, (on) => this.api.copy(share, path, to, { at }, on))) {
      if (share === this.browser.share() && dir === this.browser.path()) await this.browser.reload();
    }
  }

  /**
   * Runs what the server does as a copy — a copy, an unpacking, an archive
   * written to the share — with its progress in the panel, and says how it
   * went; true when it ended whole.
   */
  private async copyOne(
    share: string,
    to: string,
    name: string,
    run: (onProgress: (p: CopyProgress) => void) => Promise<CopyResult>,
    stays?: string,
  ): Promise<boolean> {
    const dir = parent(to);
    const where = dir ? dir.slice(dir.lastIndexOf('/') + 1) : share;
    let job: CopyJob | null = null;
    try {
      const r = await run((p) => {
        job ??= this.transfers.trackCopy(share, dir, to.slice(to.lastIndexOf('/') + 1), where, p.totalBytes);
        job.progress(p.bytes);
      });
      job ??= this.transfers.trackCopy(
        share,
        dir,
        to.slice(to.lastIndexOf('/') + 1),
        where,
        r.progress.totalBytes,
      );
      if (r.entry) {
        job.finish();
        this.toasts.show($localize`:@@copy.done:Copied: ${r.entry.name}:name:`);
        return true;
      }
      const p = r.progress;
      let why = $localize`:@@copy.partly:${name}:name: stopped after ${this.format.count(p.files)}:done: of ${this.format.count(p.totalFiles)}:total: items: ${describe(r.error)}:why:`;
      // What was made before stays: nothing is deleted to undo it.
      if (stays) why += ' ' + stays;
      job.finish(why);
      this.toasts.error(why);
    } catch (err) {
      this.toasts.error(`${name}: ${describe(err)}`);
    }
    return false;
  }

  /**
   * A name in dir for a copy of name: with suffix, "name (suffix).ext",
   * and "(suffix 2)" and on while one is taken; without, name itself.
   */
  private async freeName(share: string, dir: string, name: string, suffix: string | null): Promise<string> {
    if (suffix === null) return join(dir, name);
    const dot = name.lastIndexOf('.');
    const [stem, ext] = dot > 0 ? [name.slice(0, dot), name.slice(dot)] : [name, ''];
    let taken = new Set<string>();
    try {
      taken = new Set((await this.api.entries(share, dir)).entries.map((e) => e.name));
    } catch {
      // The server says so when the copy is refused.
    }
    for (let i = 1; ; i++) {
      const candidate = `${stem} (${suffix}${i > 1 ? ` ${i}` : ''})${ext}`;
      if (!taken.has(candidate)) return join(dir, candidate);
    }
  }

  /** Deletes, after asking. A folder goes with everything in it, which the question says. */
  async remove(entries: Entry[]): Promise<boolean> {
    const share = this.browser.share();
    if (share === null || !entries.length) return false;
    // FERRY_MAX_DELETE bounds what one delete removes: the server counts a
    // folder's contents, and the interface holds the selection to the same
    // number, so that the bound means one thing whichever way it is reached.
    const max = this.session.info()?.limits?.delete;
    if (max && entries.length > max) {
      this.toasts.error(
        $localize`:@@delete.tooMany:${this.format.count(entries.length)}:n: items are more than ferry deletes at once (${this.format.count(max)}:max:). Select fewer.`,
      );
      return false;
    }
    const folders = entries.some((e) => e.type === 'folder');
    const ok = await this.dialogs.confirm({
      title:
        entries.length === 1
          ? $localize`:@@delete.title1:Delete “${entries[0].name}:name:”?`
          : $localize`:@@delete.titleN:Delete ${this.format.count(entries.length)}:n: items?`,
      text: folders
        ? $localize`:@@delete.textFolders:Folders are deleted with everything in them. Deleting is final; it can only be brought back from a snapshot, if the NAS keeps them.`
        : $localize`:@@delete.text:Deleting is final. It can only be brought back from a snapshot, if the NAS keeps them.`,
      ok: $localize`:@@delete.ok:Delete`,
      danger: true,
    });
    if (!ok) return false;
    let deleted = 0;
    const failures: string[] = [];
    for (const e of entries) {
      try {
        await this.api.remove(share, e.path, e.type === 'folder');
        deleted++;
      } catch (err) {
        failures.push(`${e.name}: ${describe(err, 'delete')}`);
      }
    }
    this.browser.clear();
    await this.browser.reload();
    if (failures.length) {
      this.toasts.error(
        failures.length === 1
          ? failures[0]
          : $localize`:@@delete.someFailed:${failures.length}:n: items could not be deleted.`,
      );
    }
    if (deleted) {
      this.toasts.show(
        deleted === 1
          ? $localize`:@@delete.one:Deleted.`
          : $localize`:@@delete.many:${deleted}:n: items deleted.`,
      );
    }
    return deleted > 0;
  }
}
