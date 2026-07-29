import { describe, it, expect } from 'vitest'
import { computeBucket } from './bucket.js'

describe('computeBucket', () => {
  it('returns value in [0,1)', () => {
    for (const key of ['user-1', 'user-2', 'alice', 'bob', 'test-key']) {
      const b = computeBucket({ key }, 'key', 'my-flag', 'my-salt')
      expect(b).toBeGreaterThanOrEqual(0)
      expect(b).toBeLessThan(1)
    }
  })

  it('is deterministic', () => {
    const ctx = { key: 'user-123' }
    const b1 = computeBucket(ctx, 'key', 'flag', 'salt')
    const b2 = computeBucket(ctx, 'key', 'flag', 'salt')
    expect(b1).toBe(b2)
  })

  it('seed changes assignment', () => {
    const ctx = { key: 'user-abc' }
    const b1 = computeBucket(ctx, 'key', 'flag', 'salt', undefined)
    const b2 = computeBucket(ctx, 'key', 'flag', 'salt', 42)
    expect(b1).not.toBe(b2)
  })

  it('returns 0 for missing attribute', () => {
    const ctx = { key: 'user-1' }
    const b = computeBucket(ctx, 'nonexistent', 'flag', 'salt')
    expect(b).toBe(0)
  })

  it('returns 0 for non-integral float attribute', () => {
    const ctx = { key: 'u', attributes: { weight: 1.5 } }
    const b = computeBucket(ctx, 'weight', 'flag', 'salt')
    expect(b).toBe(0)
  })

  it('handles integral float attribute', () => {
    const ctx = { key: 'u', attributes: { id: 42.0 } }
    const b = computeBucket(ctx, 'id', 'flag', 'salt')
    expect(b).toBeGreaterThanOrEqual(0)
    expect(b).toBeLessThan(1)
  })

  it('produces uniform distribution', () => {
    // 10,000 users should be roughly uniformly distributed
    const buckets = new Array(10).fill(0)
    for (let i = 0; i < 10000; i++) {
      const b = computeBucket({ key: `user-${i}` }, 'key', 'test-flag', 'test-salt')
      buckets[Math.floor(b * 10)]++
    }
    for (const count of buckets) {
      expect(count).toBeGreaterThan(700)  // >7% per bucket
      expect(count).toBeLessThan(1300)    // <13% per bucket
    }
  })

  it('known value: user-4821, flag=my-flag, salt=salt123 is deterministic across calls', () => {
    const ctx = { key: 'user-4821' }
    const first = computeBucket(ctx, 'key', 'my-flag', 'salt123')
    for (let i = 0; i < 100; i++) {
      expect(computeBucket(ctx, 'key', 'my-flag', 'salt123')).toBe(first)
    }
  })

  it('flags with different keys produce independent buckets', () => {
    let bothLow = 0, aLow = 0
    for (let i = 0; i < 10000; i++) {
      const ctx = { key: `user-${i}` }
      const a = computeBucket(ctx, 'key', 'flag-a', 'salt-a')
      const b = computeBucket(ctx, 'key', 'flag-b', 'salt-b')
      if (a < 0.1) {
        aLow++
        if (b < 0.1) bothLow++
      }
    }
    const ratio = bothLow / aLow
    // Should be close to 0.1 (independent)
    expect(ratio).toBeGreaterThan(0.07)
    expect(ratio).toBeLessThan(0.13)
  })
})
