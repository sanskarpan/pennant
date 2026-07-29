/**
 * Conformance tests: runs against the JSON fixtures in ../../conformance/
 * to ensure the TypeScript SDK produces identical results to the Go engine.
 */
import { describe, it, expect } from 'vitest'
import { readFileSync, readdirSync, statSync } from 'fs'
import { join, resolve } from 'path'
import { fileURLToPath } from 'url'
import { evaluate } from './evaluate.js'
import type { EvalStore } from './evaluate.js'
import type { Flag, FlagConfig, Segment, EvalContext } from './types.js'

const __dirname = fileURLToPath(new URL('.', import.meta.url))
// From src/ go up: src -> ts -> sdk -> Feature-flags, then into conformance/
const CONFORMANCE_DIR = resolve(__dirname, '../../..', 'conformance')

interface FlagEntry {
  flag: Flag
  flagConfig: FlagConfig
}

interface ConformanceFixture {
  description: string
  flag: Flag
  flagConfig: FlagConfig
  context: EvalContext
  // segments map: key -> Segment (used by segmentMatch clauses)
  segments?: Record<string, Segment>
  // prerequisites map: flag_key -> FlagEntry (used by prerequisite evaluation)
  prerequisites?: Record<string, FlagEntry>
  expected: {
    variationIndex: number | null
    value: unknown
    reason: string
    errorKind: string | null
  }
}

function loadFixtures(dir: string): Array<{ file: string; fixture: ConformanceFixture }> {
  const results: Array<{ file: string; fixture: ConformanceFixture }> = []
  let entries: string[]
  try {
    entries = readdirSync(dir)
  } catch {
    return results
  }
  for (const entry of entries) {
    const fullPath = join(dir, entry)
    let stat
    try {
      stat = statSync(fullPath)
    } catch {
      continue
    }
    if (stat.isDirectory()) {
      results.push(...loadFixtures(fullPath))
    } else if (entry.endsWith('.json')) {
      try {
        const content = readFileSync(fullPath, 'utf8')
        const fixture = JSON.parse(content) as ConformanceFixture
        results.push({
          file: fullPath.replace(CONFORMANCE_DIR + '/', ''),
          fixture,
        })
      } catch (e) {
        console.warn(`Failed to parse ${fullPath}: ${e}`)
      }
    }
  }
  return results
}

const fixtures = loadFixtures(CONFORMANCE_DIR)

function makeStore(fixture: ConformanceFixture): EvalStore {
  const segments = fixture.segments ?? {}
  const prereqs = fixture.prerequisites ?? {}

  return {
    getFlag: (key: string) => {
      const entry = prereqs[key]
      if (!entry) return undefined
      return { flag: entry.flag, config: entry.flagConfig }
    },
    getSegment: (key: string) => segments[key],
  }
}

if (fixtures.length === 0) {
  describe('conformance', () => {
    it.skip('no conformance fixtures found — skipping', () => {})
  })
} else {
  describe('conformance fixtures', () => {
    for (const { file, fixture } of fixtures) {
      it(`${file}: ${fixture.description}`, () => {
        const store = makeStore(fixture)
        const result = evaluate(fixture.flag, fixture.flagConfig, fixture.context, store)

        expect(result.variationIndex, 'variationIndex').toBe(fixture.expected.variationIndex)
        expect(result.value, 'value').toStrictEqual(fixture.expected.value)
        expect(result.reason.kind, 'reason.kind').toBe(fixture.expected.reason)

        if (fixture.expected.errorKind !== null && fixture.expected.errorKind !== undefined) {
          expect(result.reason.errorKind, 'reason.errorKind').toBe(fixture.expected.errorKind)
        } else {
          expect(result.reason.errorKind, 'reason.errorKind should be absent').toBeUndefined()
        }
      })
    }
  })
}
