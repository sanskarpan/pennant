import { evaluate } from './evaluate.js'
import type { EvalContext, EvalResult, Flag, FlagConfig, Segment, Snapshot } from './types.js'
import type { EvalStore } from './evaluate.js'

// SnapshotStore wraps a Snapshot and implements EvalStore for evaluation.
class SnapshotStore implements EvalStore {
  constructor(private snapshot: Snapshot) {}

  getFlag(key: string): { flag: Flag; config: FlagConfig } | undefined {
    const rf = this.snapshot.flags[key]
    if (!rf) return undefined
    return {
      flag: {
        key: rf.key,
        name: rf.name,
        description: rf.description,
        type: rf.type,
        variations: rf.variations,
        tags: rf.tags,
        temporary: rf.temporary,
        archived: rf.archived,
      },
      config: rf.config,
    }
  }

  getSegment(key: string): Segment | undefined {
    return this.snapshot.segments[key]
  }
}

export interface ClientOptions {
  sdkKey: string
  streamUrl?: string
  onSnapshotUpdate?: (snap: Snapshot) => void
}

// PennantClient evaluates flags against an in-memory snapshot.
// The snapshot is updated atomically on SSE events.
export class PennantClient {
  private snapshot: Snapshot | null = null
  private store: SnapshotStore | null = null
  private eventSource: EventSource | null = null
  private opts: ClientOptions

  constructor(opts: ClientOptions) {
    this.opts = opts
  }

  // init fetches the initial snapshot and subscribes to SSE updates.
  async init(streamUrl: string): Promise<void> {
    const resp = await fetch(`${streamUrl}/sdk/v1/snapshot`, {
      headers: { Authorization: `Bearer ${this.opts.sdkKey}` },
    })
    if (!resp.ok) {
      throw new Error(`Failed to fetch snapshot: ${resp.status}`)
    }
    const snap: Snapshot = await resp.json() as Snapshot
    this.setSnapshot(snap)

    if (typeof EventSource !== 'undefined') {
      this.connectSSE(streamUrl)
    }
  }

  private setSnapshot(snap: Snapshot): void {
    this.snapshot = snap
    this.store = new SnapshotStore(snap)
    this.opts.onSnapshotUpdate?.(snap)
  }

  private connectSSE(streamUrl: string): void {
    const url = `${streamUrl}/sdk/v1/stream?sdkKey=${encodeURIComponent(this.opts.sdkKey)}`
    const es = new EventSource(url)

    es.addEventListener('put', (e) => {
      const snap: Snapshot = JSON.parse((e as MessageEvent).data) as Snapshot
      this.setSnapshot(snap)
    })

    es.addEventListener('patch', (e) => {
      // Apply delta to current snapshot
      try {
        const snap: Snapshot = JSON.parse((e as MessageEvent).data) as Snapshot
        this.setSnapshot(snap)
      } catch {
        // ignore malformed patch events
      }
    })

    es.onerror = () => {
      // Browser SSE auto-reconnects; nothing to do here
    }

    this.eventSource = es
  }

  variation(flagKey: string, ctx: EvalContext, defaultValue: unknown): unknown {
    const result = this.evaluateDetail(flagKey, ctx)
    if (result === null) return defaultValue
    return result.value ?? defaultValue
  }

  evaluateDetail(flagKey: string, ctx: EvalContext): EvalResult | null {
    if (!this.store || !this.snapshot) return null
    const entry = this.store.getFlag(flagKey)
    if (!entry) return null
    return evaluate(entry.flag, entry.config, ctx, this.store)
  }

  getSnapshot(): Snapshot | null { return this.snapshot }

  close(): void {
    this.eventSource?.close()
  }
}
