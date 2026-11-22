import { Injectable, inject } from '@angular/core';
import { Api, Entry, join } from './api';
import { Browser } from './browser';
import { describe } from './errors';
import { Format } from './format';
import { Session } from './session';
import { Dialogs, checkName } from '../ui/dialog';
import { Toasts } from '../ui/toast';
import { FolderPicker } from '../ui/folder-picker';

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
    if (dest === null || dest === this.browser.path()) return;
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
