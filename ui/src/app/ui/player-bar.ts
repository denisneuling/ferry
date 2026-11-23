import { ChangeDetectionStrategy, Component, effect, inject } from '@angular/core';
import { Player } from '../core/player';
import { Icon } from './icon';
import { duration } from './file-details';

/** The playlist playing, along the bottom: on while the person browses. */
@Component({
  selector: 'f-player',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [Icon],
  template: `@if (p.track(); as t) {
    <div class="player" role="region" aria-label="Player" i18n-aria-label="@@player.label">
      @if (p.cover(t); as src) {
        <img class="player-cover" [src]="src" alt="" />
      } @else {
        <span class="player-cover"><f-icon name="audio" [size]="20" /></span>
      }
      <div class="player-what">
        <b>{{ t.name }}</b>
        <small>{{ p.queue()?.folder }} · {{ clock(p.position()) }} / {{ clock(p.duration()) }}</small>
        <input
          type="range"
          min="0"
          [max]="p.duration() || 0"
          step="1"
          [value]="p.position()"
          (change)="p.seek(+seekbar.value)"
          #seekbar
          aria-label="Position"
          i18n-aria-label="@@player.position"
        />
      </div>
      <button
        type="button"
        class="icon-btn"
        (click)="p.previous()"
        title="Previous"
        i18n-title="@@player.prev"
      >
        <f-icon name="left" [size]="18" />
      </button>
      <button
        type="button"
        class="icon-btn play"
        (click)="p.toggle()"
        [title]="p.playing() ? pauseText : playText"
      >
        <f-icon [name]="p.playing() ? 'pause' : 'play'" [size]="20" />
      </button>
      <button type="button" class="icon-btn" (click)="p.next()" title="Next" i18n-title="@@player.next">
        <f-icon name="right" [size]="18" />
      </button>
      <button
        type="button"
        class="icon-btn"
        [class.on]="p.shuffle()"
        [attr.aria-pressed]="p.shuffle()"
        (click)="p.setShuffle(!p.shuffle())"
        title="Shuffle"
        i18n-title="@@player.shuffle"
      >
        <f-icon name="shuffle" [size]="17" />
      </button>
      <button type="button" class="icon-btn" (click)="p.stop()" title="Stop" i18n-title="@@player.stop">
        <f-icon name="x" [size]="17" />
      </button>
    </div>
  }`,
})
export class PlayerBar {
  protected readonly p = inject(Player);
  protected readonly playText = $localize`:@@player.play:Play`;
  protected readonly pauseText = $localize`:@@player.pause:Pause`;

  constructor() {
    // The panels at the bottom right make room while the player shows.
    effect(() => document.body.classList.toggle('fx-playing', this.p.track() !== null));
  }

  protected clock(s: number): string {
    return duration(s || 0);
  }
}
