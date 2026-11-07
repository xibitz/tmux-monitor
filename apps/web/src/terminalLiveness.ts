export const TERMINAL_LEASE_MS = 15_000;

// A queued heartbeat must never revive an expired viewer after sleep or timer
// throttling. All input paths check this deadline synchronously as well.
export class TerminalLiveness {
  private last: number;
  private deadline: number;
  private stopped = false;
  private readonly now: () => number;

  constructor(now: () => number = Date.now) {
    this.now = now;
    this.last = now();
    this.deadline = this.last + TERMINAL_LEASE_MS;
  }

  current(): boolean {
    const time = this.now();
    if (this.stopped || time < this.last || time >= this.deadline) {
      this.stopped = true;
      return false;
    }
    this.last = time;
    return true;
  }

  heartbeat(): boolean {
    if (!this.current()) return false;
    this.deadline = this.last + TERMINAL_LEASE_MS;
    return true;
  }

  stop() {
    this.stopped = true;
  }
}

// Connectivity loss permanently invalidates a pending mode replacement, even
// between the expected mode_changed close and the arrival of its REST response.
export class TerminalReplacement {
  private allowed = true;
  private started: number | undefined;
  private readonly now: () => number;

  constructor(now: () => number = Date.now) {
    this.now = now;
  }

  begin() {
    if (this.allowed && this.started === undefined) this.started = this.now();
  }

  pending(): boolean {
    return this.allowed && this.started !== undefined;
  }

  current(): boolean {
    if (!this.pending()) return false;
    const elapsed = this.now() - this.started!;
    if (elapsed < 0 || elapsed >= TERMINAL_LEASE_MS) this.invalidate();
    return this.allowed;
  }

  invalidate() {
    this.allowed = false;
  }
}
