// Keep intentional pause/resume clicks distinct even when the OS accepts a slower native double click.
export const PLAYBACK_DOUBLE_CLICK_WINDOW = 250;

type Click = Pick<MouseEvent, 'detail' | 'timeStamp'>;

export class PlaybackClickSequence {
  private first: { time: number; paused: boolean } | null = null;
  private pairedPaused: boolean | null = null;

  /** Whether this click should act immediately. Only a rapid second native click is held for dblclick. */
  public click(event: Click, paused: boolean): boolean {
    const first = this.first;
    this.reset();
    const elapsed = first ? event.timeStamp - first.time : -1;
    if (first && event.detail > 1 && event.detail % 2 === 0 && elapsed >= 0 && elapsed <= PLAYBACK_DOUBLE_CLICK_WINDOW) {
      this.pairedPaused = first.paused;
      return false;
    }
    if (event.detail !== 0) {
      this.first = { time: event.timeStamp, paused };
    }
    return true;
  }

  /** The state to restore before seeking, or null when native dblclick paired two separate actions. */
  public doubleClick(): boolean | null {
    const paused = this.pairedPaused;
    this.reset();
    return paused;
  }

  public reset() {
    this.first = null;
    this.pairedPaused = null;
  }
}
