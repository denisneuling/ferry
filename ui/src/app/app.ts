import { ChangeDetectionStrategy, Component, inject } from '@angular/core';
import { Api } from './core/api';
import { setPreviews } from './core/kinds';
import { Prefs } from './core/prefs';
import { Session } from './core/session';
import { Login } from './pages/login';
import { Shell } from './pages/shell';
import { DialogHost } from './ui/dialog';
import { FolderPickerHost } from './ui/folder-picker';
import { ToastHost } from './ui/toast';
import { VersionsHost } from './ui/versions';
import { BatchRenameHost } from './ui/batch-rename';

/** The interface: the login, or the shell once a session exists; dialogs and toasts over both. */
@Component({
  selector: 'f-root',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Login, Shell, DialogHost, FolderPickerHost, ToastHost, VersionsHost, BatchRenameHost],
  template: `@switch (session.me()) {
      @case (undefined) {}
      @case (null) {
        <f-login />
      }
      @default {
        <f-shell />
      }
    }
    <f-dialogs />
    <f-folder-picker />
    <f-versions />
    <f-batch-rename />
    <f-toasts />`,
})
export class App {
  protected readonly session = inject(Session);
  private readonly api = inject(Api);

  constructor() {
    inject(Prefs); // applies the theme from the start
    void this.start();
  }

  /** Is there a session already? The cookie is HttpOnly: only the server can say. */
  private async start(): Promise<void> {
    this.api.info().then(
      (i) => {
        this.session.info.set(i);
        setPreviews(i.previews);
      },
      () => undefined,
    );
    try {
      this.session.started(await this.api.me());
    } catch {
      this.session.me.set(null);
    }
  }
}
