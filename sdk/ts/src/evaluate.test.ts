import { describe, it, expect } from 'vitest'
import { evaluate } from './evaluate.js'
import type { EvalStore } from './evaluate.js'
import type { Flag, FlagConfig, Segment } from './types.js'

function makeStore(
  flags: Record<string, { flag: Flag; config: FlagConfig }>,
  segments: Record<string, Segment> = {},
): EvalStore {
  return {
    getFlag: (key) => flags[key],
    getSegment: (key) => segments[key],
  }
}

// boolFlag creates a minimal boolean flag with two variations: false(0), true(1).
function boolFlag(
  key: string,
  on: boolean,
  offVar: number | null = 0,
  fallthroughVar = 1,
): { flag: Flag; config: FlagConfig } {
  return {
    flag: {
      key,
      name: key,
      type: 'boolean',
      variations: [
        { id: 'v0', value: false },
        { id: 'v1', value: true },
      ],
    },
    config: {
      on,
      off_variation: offVar,
      prerequisites: [],
      targets: [],
      rules: [],
      fallthrough: { variation: fallthroughVar },
      salt: key,
    },
  }
}

describe('evaluate — OFF state', () => {
  it('returns OFF with off variation when flag is off', () => {
    const { flag, config } = boolFlag('f', false, 0)
    const result = evaluate(flag, config, { key: 'u1' }, makeStore({}))
    expect(result.reason.kind).toBe('OFF')
    expect(result.variationIndex).toBe(0)
    expect(result.value).toBe(false)
  })

  it('returns null variation when off and no off_variation', () => {
    const { flag, config } = boolFlag('f', false, null)
    const result = evaluate(flag, config, { key: 'u1' }, makeStore({}))
    expect(result.reason.kind).toBe('OFF')
    expect(result.variationIndex).toBeNull()
    expect(result.value).toBeNull()
  })
})

describe('evaluate — FALLTHROUGH', () => {
  it('returns FALLTHROUGH when flag is on and no targets/rules match', () => {
    const { flag, config } = boolFlag('f', true, 0, 1)
    const result = evaluate(flag, config, { key: 'u1' }, makeStore({}))
    expect(result.reason.kind).toBe('FALLTHROUGH')
    expect(result.variationIndex).toBe(1)
    expect(result.value).toBe(true)
  })
})

describe('evaluate — individual targets', () => {
  it('target match returns TARGET_MATCH', () => {
    const { flag, config } = boolFlag('f', true, 0, 0)
    config.targets = [{ context_keys: ['alice'], variation: 1 }]
    const result = evaluate(flag, config, { key: 'alice' }, makeStore({}))
    expect(result.reason.kind).toBe('TARGET_MATCH')
    expect(result.variationIndex).toBe(1)
  })

  it('target wins over rules (same user matched by both)', () => {
    const { flag, config } = boolFlag('f', true, 0, 1)
    config.targets = [{ context_keys: ['alice'], variation: 0 }]
    config.rules = [{
      id: 'r1',
      clauses: [{ attribute: 'key', op: 'in', values: ['alice'], negate: false }],
      variation: 1,
    }]
    const result = evaluate(flag, config, { key: 'alice' }, makeStore({}))
    expect(result.reason.kind).toBe('TARGET_MATCH')
    expect(result.variationIndex).toBe(0)
  })

  it('non-matched target falls through to FALLTHROUGH', () => {
    const { flag, config } = boolFlag('f', true, 0, 0)
    config.targets = [{ context_keys: ['alice'], variation: 1 }]
    const result = evaluate(flag, config, { key: 'bob' }, makeStore({}))
    expect(result.reason.kind).toBe('FALLTHROUGH')
    expect(result.variationIndex).toBe(0)
  })
})

describe('evaluate — rules', () => {
  it('first matching rule wins', () => {
    const { flag, config } = boolFlag('f', true, 0, 0)
    config.rules = [
      { id: 'r1', clauses: [{ attribute: 'plan', op: 'in', values: ['enterprise'], negate: false }], variation: 1 },
      { id: 'r2', clauses: [{ attribute: 'plan', op: 'in', values: ['enterprise'], negate: false }], variation: 0 },
    ]
    const result = evaluate(flag, config, { key: 'u1', attributes: { plan: 'enterprise' } }, makeStore({}))
    expect(result.reason.kind).toBe('RULE_MATCH')
    expect(result.reason.ruleIndex).toBe(0)
    expect(result.reason.ruleID).toBe('r1')
    expect(result.variationIndex).toBe(1)
  })

  it('empty clause list does not match', () => {
    const { flag, config } = boolFlag('f', true, 0, 0)
    config.rules = [{ id: 'r1', clauses: [], variation: 1 }]
    const result = evaluate(flag, config, { key: 'u1' }, makeStore({}))
    expect(result.reason.kind).toBe('FALLTHROUGH')
  })

  it('all clauses must match (AND semantics)', () => {
    const { flag, config } = boolFlag('f', true, 0, 0)
    config.rules = [{
      id: 'r1',
      clauses: [
        { attribute: 'plan', op: 'in', values: ['enterprise'], negate: false },
        { attribute: 'country', op: 'in', values: ['US'], negate: false },
      ],
      variation: 1,
    }]
    // Both match
    expect(evaluate(flag, config, { key: 'u1', attributes: { plan: 'enterprise', country: 'US' } }, makeStore({})).reason.kind).toBe('RULE_MATCH')
    // Only first matches
    expect(evaluate(flag, config, { key: 'u2', attributes: { plan: 'enterprise', country: 'CA' } }, makeStore({})).reason.kind).toBe('FALLTHROUGH')
  })
})

describe('evaluate — missing attribute + negation', () => {
  it('missing attribute returns false BEFORE negation is applied', () => {
    // negate=true on missing attr: missing → false (not negated to true)
    const { flag, config } = boolFlag('f', true, 0, 1)
    config.rules = [{
      id: 'r1',
      clauses: [{ attribute: 'plan', op: 'in', values: ['free'], negate: true }],
      variation: 0,
    }]
    // User has no 'plan' attribute — rule should NOT match despite negation
    const result = evaluate(flag, config, { key: 'u1' }, makeStore({}))
    expect(result.reason.kind).toBe('FALLTHROUGH')
  })
})

describe('evaluate — segment matching', () => {
  it('excluded beats included', () => {
    const { flag, config } = boolFlag('f', true, 0, 1)
    const seg: Segment = {
      key: 'beta',
      included: ['alice'],
      excluded: ['alice'],
      rules: [],
      salt: 'beta',
    }
    config.rules = [{
      id: 'r1',
      clauses: [{ attribute: '', op: 'segmentMatch', values: ['beta'], negate: false }],
      variation: 0,
    }]
    const result = evaluate(flag, config, { key: 'alice' }, makeStore({}, { beta: seg }))
    // excluded wins → segment doesn't match → rule doesn't match → FALLTHROUGH
    expect(result.reason.kind).toBe('FALLTHROUGH')
  })

  it('included user matches segment', () => {
    const { flag, config } = boolFlag('f', true, 0, 0)
    const seg: Segment = {
      key: 'beta',
      included: ['alice'],
      excluded: [],
      rules: [],
      salt: 'beta',
    }
    config.rules = [{
      id: 'r1',
      clauses: [{ attribute: '', op: 'segmentMatch', values: ['beta'], negate: false }],
      variation: 1,
    }]
    const result = evaluate(flag, config, { key: 'alice' }, makeStore({}, { beta: seg }))
    expect(result.reason.kind).toBe('RULE_MATCH')
    expect(result.variationIndex).toBe(1)
  })

  it('negated segment match: non-member passes', () => {
    const { flag, config } = boolFlag('f', true, 0, 0)
    const seg: Segment = {
      key: 'blocked',
      included: ['blocked-user'],
      excluded: [],
      rules: [],
      salt: 'blocked',
    }
    config.rules = [{
      id: 'r1',
      clauses: [{ attribute: '', op: 'segmentMatch', values: ['blocked'], negate: true }],
      variation: 1,
    }]
    // normal-user is NOT in segment, negate=true → matches rule
    const result = evaluate(flag, config, { key: 'normal-user' }, makeStore({}, { blocked: seg }))
    expect(result.reason.kind).toBe('RULE_MATCH')

    // blocked-user IS in segment, negate=true → does NOT match rule
    const result2 = evaluate(flag, config, { key: 'blocked-user' }, makeStore({}, { blocked: seg }))
    expect(result2.reason.kind).toBe('FALLTHROUGH')
  })
})

describe('evaluate — prerequisites', () => {
  it('prerequisite fail (off flag) returns PREREQUISITE_FAILED', () => {
    const { flag, config } = boolFlag('main', true, 0, 1)
    config.prerequisites = [{ flag_key: 'prereq', variation: 1 }]
    const prereqEntry = boolFlag('prereq', false, 0, 1) // off → variation 0, needs variation 1
    const result = evaluate(flag, config, { key: 'u1' }, makeStore({ prereq: prereqEntry }))
    expect(result.reason.kind).toBe('PREREQUISITE_FAILED')
    expect(result.reason.prerequisiteKey).toBe('prereq')
  })

  it('prerequisite pass: evaluation continues normally', () => {
    const { flag, config } = boolFlag('main', true, 0, 1)
    config.prerequisites = [{ flag_key: 'prereq', variation: 1 }]
    const prereqEntry = boolFlag('prereq', true, 0, 1) // on → FALLTHROUGH → variation 1
    const result = evaluate(flag, config, { key: 'u1' }, makeStore({ prereq: prereqEntry }))
    expect(result.reason.kind).toBe('FALLTHROUGH')
    expect(result.variationIndex).toBe(1)
  })

  it('missing prerequisite flag returns PREREQUISITE_FAILED', () => {
    const { flag, config } = boolFlag('main', true, 0, 1)
    config.prerequisites = [{ flag_key: 'nonexistent', variation: 1 }]
    const result = evaluate(flag, config, { key: 'u1' }, makeStore({}))
    expect(result.reason.kind).toBe('PREREQUISITE_FAILED')
    expect(result.reason.prerequisiteKey).toBe('nonexistent')
  })

  it('prerequisite wrong variation returns PREREQUISITE_FAILED', () => {
    const { flag, config } = boolFlag('main', true, 0, 1)
    config.prerequisites = [{ flag_key: 'prereq', variation: 0 }] // needs 0, will get 1
    const prereqEntry = boolFlag('prereq', true, 0, 1) // on → FALLTHROUGH → variation 1
    const result = evaluate(flag, config, { key: 'u1' }, makeStore({ prereq: prereqEntry }))
    expect(result.reason.kind).toBe('PREREQUISITE_FAILED')
  })

  it('self-referential prerequisite cycle returns PREREQUISITE_FAILED', () => {
    const { flag, config } = boolFlag('self-ref', true, 0, 1)
    config.prerequisites = [{ flag_key: 'self-ref', variation: 1 }]
    const store = makeStore({ 'self-ref': { flag, config } })
    const result = evaluate(flag, config, { key: 'u1' }, store)
    expect(result.reason.kind).toBe('PREREQUISITE_FAILED')
    expect(result.reason.prerequisiteKey).toBe('self-ref')
  })
})

describe('evaluate — rollout', () => {
  it('100% weight to variation 1 always returns 1', () => {
    const { flag, config } = boolFlag('f', true, 0, 1)
    config.fallthrough = {
      rollout: {
        variations: [
          { variation: 0, weight: 0 },
          { variation: 1, weight: 100000 },
        ],
        bucket_by: 'key',
      },
    }
    for (let i = 0; i < 100; i++) {
      const result = evaluate(flag, config, { key: `user-${i}` }, makeStore({}))
      expect(result.variationIndex).toBe(1)
    }
  })

  it('50/50 rollout distributes approximately equally', () => {
    const { flag, config } = boolFlag('rollout-flag', true, 0, 0)
    config.salt = 'test-salt'
    config.fallthrough = {
      rollout: {
        variations: [
          { variation: 0, weight: 50000 },
          { variation: 1, weight: 50000 },
        ],
        bucket_by: 'key',
      },
    }
    const counts = [0, 0]
    for (let i = 0; i < 10000; i++) {
      const result = evaluate(flag, config, { key: `user-${i}` }, makeStore({}))
      if (result.variationIndex !== null) counts[result.variationIndex]++
    }
    const ratio = counts[0] / (counts[0] + counts[1])
    expect(ratio).toBeGreaterThan(0.45)
    expect(ratio).toBeLessThan(0.55)
  })

  it('empty rollout variations returns MALFORMED_FLAG error', () => {
    const { flag, config } = boolFlag('bad-flag', true, 0, 0)
    config.fallthrough = {
      rollout: { variations: [], bucket_by: 'key' },
    }
    const result = evaluate(flag, config, { key: 'u1' }, makeStore({}))
    expect(result.reason.kind).toBe('ERROR')
    expect(result.reason.errorKind).toBe('MALFORMED_FLAG')
  })
})

describe('clause operators', () => {
  function clauseFlag(attrVal: unknown, op: string, clauseVals: unknown[], negate = false): boolean {
    const { flag, config } = boolFlag('f', true, 0, 0)
    config.rules = [{
      id: 'r1',
      clauses: [{ attribute: 'attr', op: op as never, values: clauseVals, negate }],
      variation: 1,
    }]
    const result = evaluate(flag, config, { key: 'u1', attributes: { attr: attrVal } }, makeStore({}))
    return result.reason.kind === 'RULE_MATCH'
  }

  it('in: string match', () => {
    expect(clauseFlag('alice@example.com', 'in', ['alice@example.com', 'bob@example.com'])).toBe(true)
    expect(clauseFlag('charlie@example.com', 'in', ['alice@example.com'])).toBe(false)
  })

  it('endsWith', () => {
    expect(clauseFlag('alice@acme.com', 'endsWith', ['@acme.com'])).toBe(true)
    expect(clauseFlag('alice@other.com', 'endsWith', ['@acme.com'])).toBe(false)
  })

  it('startsWith', () => {
    expect(clauseFlag('alice@acme.com', 'startsWith', ['alice'])).toBe(true)
    expect(clauseFlag('bob@acme.com', 'startsWith', ['alice'])).toBe(false)
  })

  it('contains', () => {
    expect(clauseFlag('alice@acme.com', 'contains', ['acme'])).toBe(true)
    expect(clauseFlag('alice@other.com', 'contains', ['acme'])).toBe(false)
  })

  it('matches regex', () => {
    expect(clauseFlag('test+123@example.com', 'matches', ['^test\\+[0-9]+@example\\.com$'])).toBe(true)
    expect(clauseFlag('admin@example.com', 'matches', ['^test\\+[0-9]+@example\\.com$'])).toBe(false)
  })

  it('lessThan', () => {
    expect(clauseFlag(25, 'lessThan', [30])).toBe(true)
    expect(clauseFlag(25, 'lessThan', [25])).toBe(false)
    expect(clauseFlag(30, 'lessThan', [25])).toBe(false)
  })

  it('lessThanOrEqual', () => {
    expect(clauseFlag(25, 'lessThanOrEqual', [25])).toBe(true)
    expect(clauseFlag(26, 'lessThanOrEqual', [25])).toBe(false)
  })

  it('greaterThan', () => {
    expect(clauseFlag(25, 'greaterThan', [20])).toBe(true)
    expect(clauseFlag(25, 'greaterThan', [25])).toBe(false)
  })

  it('greaterThanOrEqual', () => {
    expect(clauseFlag(25, 'greaterThanOrEqual', [25])).toBe(true)
    expect(clauseFlag(24, 'greaterThanOrEqual', [25])).toBe(false)
  })

  it('before datetime', () => {
    expect(clauseFlag('2024-01-01T00:00:00Z', 'before', ['2025-01-01T00:00:00Z'])).toBe(true)
    expect(clauseFlag('2026-01-01T00:00:00Z', 'before', ['2025-01-01T00:00:00Z'])).toBe(false)
  })

  it('after datetime', () => {
    expect(clauseFlag('2026-01-01T00:00:00Z', 'after', ['2025-01-01T00:00:00Z'])).toBe(true)
    expect(clauseFlag('2024-01-01T00:00:00Z', 'after', ['2025-01-01T00:00:00Z'])).toBe(false)
  })

  it('semVerEqual', () => {
    expect(clauseFlag('2.1.0', 'semVerEqual', ['2.1.0'])).toBe(true)
    expect(clauseFlag('2.1.0', 'semVerEqual', ['2.1.1'])).toBe(false)
  })

  it('semVerLessThan', () => {
    expect(clauseFlag('2.1.0', 'semVerLessThan', ['3.0.0'])).toBe(true)
    expect(clauseFlag('2.1.0', 'semVerLessThan', ['2.0.0'])).toBe(false)
  })

  it('semVerGreaterThan', () => {
    expect(clauseFlag('3.0.0', 'semVerGreaterThan', ['2.1.0'])).toBe(true)
    expect(clauseFlag('1.0.0', 'semVerGreaterThan', ['2.0.0'])).toBe(false)
  })

  it('array attribute OR semantics', () => {
    const { flag, config } = boolFlag('f', true, 0, 0)
    config.rules = [{
      id: 'r1',
      clauses: [{ attribute: 'roles', op: 'in', values: ['admin'], negate: false }],
      variation: 1,
    }]
    // admin in the array → matches
    const r1 = evaluate(flag, config, { key: 'u1', attributes: { roles: ['viewer', 'admin'] } }, makeStore({}))
    expect(r1.reason.kind).toBe('RULE_MATCH')

    // no admin in array → no match
    const r2 = evaluate(flag, config, { key: 'u2', attributes: { roles: ['viewer', 'editor'] } }, makeStore({}))
    expect(r2.reason.kind).toBe('FALLTHROUGH')
  })

  it('builtin key attribute', () => {
    const { flag, config } = boolFlag('f', true, 0, 0)
    config.rules = [{
      id: 'r1',
      clauses: [{ attribute: 'key', op: 'in', values: ['alice'], negate: false }],
      variation: 1,
    }]
    expect(evaluate(flag, config, { key: 'alice' }, makeStore({})).reason.kind).toBe('RULE_MATCH')
    expect(evaluate(flag, config, { key: 'bob' }, makeStore({})).reason.kind).toBe('FALLTHROUGH')
  })

  it('builtin kind attribute', () => {
    const { flag, config } = boolFlag('f', true, 0, 0)
    config.rules = [{
      id: 'r1',
      clauses: [{ attribute: 'kind', op: 'in', values: ['user'], negate: false }],
      variation: 1,
    }]
    expect(evaluate(flag, config, { key: 'u1' }, makeStore({})).reason.kind).toBe('RULE_MATCH')
    expect(evaluate(flag, config, { key: 'u2', kind: 'device' }, makeStore({})).reason.kind).toBe('FALLTHROUGH')
  })

  it('negated endsWith: non-matching attribute passes through', () => {
    const { flag, config } = boolFlag('f', true, 0, 0)
    config.rules = [{
      id: 'r1',
      clauses: [{ attribute: 'email', op: 'endsWith', values: ['@acme.com'], negate: true }],
      variation: 1,
    }]
    // ends with @other.com, not @acme.com → negation true → rule matches
    expect(evaluate(flag, config, { key: 'u1', attributes: { email: 'alice@other.com' } }, makeStore({})).reason.kind).toBe('RULE_MATCH')
    // ends with @acme.com → negation false → rule doesn't match
    expect(evaluate(flag, config, { key: 'u2', attributes: { email: 'bob@acme.com' } }, makeStore({})).reason.kind).toBe('FALLTHROUGH')
  })
})
