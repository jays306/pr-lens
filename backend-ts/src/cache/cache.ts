import { createHash } from "node:crypto";
import type { StreamEvent } from "../analysis/types.js";

export const VERSION = "v4";

interface CacheEntry {
  events: StreamEvent[];
  createdAt: number;
}

export function cacheKey(...parts: string[]): string {
  const h = createHash("sha256");
  for (const p of parts) {
    h.update(p);
    h.update("\0");
  }
  return `${VERSION}:${h.digest("hex")}`;
}

export class Cache {
  private store = new Map<string, CacheEntry>();
  private ttlMs: number;

  constructor(ttlMs = 24 * 60 * 60 * 1000) {
    this.ttlMs = ttlMs;
  }

  get(key: string): StreamEvent[] | null {
    const entry = this.store.get(key);
    if (!entry) return null;
    if (Date.now() - entry.createdAt > this.ttlMs) {
      this.store.delete(key);
      return null;
    }
    return entry.events;
  }

  set(key: string, events: StreamEvent[]): void {
    this.store.set(key, { events, createdAt: Date.now() });
  }

  /** Wrap an emit function to collect all events, then store them on commit(). */
  collect(emit: (ev: StreamEvent) => void): {
    collectingEmit: (ev: StreamEvent) => void;
    commit: (key: string) => void;
  } {
    const collected: StreamEvent[] = [];
    return {
      collectingEmit: (ev) => {
        collected.push(ev);
        emit(ev);
      },
      commit: (key) => this.set(key, collected),
    };
  }

  /** Replay cached events through emit, skipping "done" events. */
  async replay(events: StreamEvent[], emit: (ev: StreamEvent) => void | Promise<void>): Promise<void> {
    for (const ev of events) {
      if (ev.type !== "done") await emit(ev);
    }
  }

  logStats(key: string, hit: boolean): void {
    console.log(`[cache] ${hit ? "HIT" : "MISS"} key=${key.slice(0, 20)}…`);
  }
}
