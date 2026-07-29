import { computeBucket } from './bucket.js'
import { matchSingle } from './operators.js'
import type {
  Clause,
  EvalContext,
  EvalResult,
  Flag,
  FlagConfig,
  Reason,
  Rollout,
  Rule,
  Segment,
  SegmentRule,
  VariationOrRollout,
} from './types.js'

const MAX_PREREQUISITE_DEPTH = 20

export interface EvalStore {
  getFlag(key: string): { flag: Flag; config: FlagConfig } | undefined
  getSegment(key: string): Segment | undefined
}

// evaluate is the main entry point. It mirrors Go's Evaluate / evaluateWithDepth.
export function evaluate(
  flag: Flag,
  config: FlagConfig,
  ctx: EvalContext,
  store: EvalStore,
  depth = 0,
  visited = new Set<string>(),
): EvalResult {
  // Helper: return the off-variation result (or null variation if off_variation not set)
  const offResult = (reason: Reason): EvalResult => {
    if (config.off_variation === null || config.off_variation === undefined) {
      return { variationIndex: null, value: null, reason }
    }
    return variationResult(flag, config.off_variation, reason)
  }

  // STEP 1: Flag off
  if (!config.on) {
    return offResult({ kind: 'OFF' })
  }

  // STEP 2: Prerequisites (cycle detection mirrors Go's visited map)
  if (depth > MAX_PREREQUISITE_DEPTH || visited.has(flag.key)) {
    return offResult({ kind: 'ERROR', errorKind: 'MALFORMED_FLAG' })
  }
  const nextVisited = new Set(visited)
  nextVisited.add(flag.key)

  if (config.prerequisites) {
    for (const prereq of config.prerequisites) {
      const entry = store.getFlag(prereq.flag_key)
      // Missing flag OR flag is off → prerequisite failed (mirrors Go behaviour)
      if (!entry || !entry.config.on) {
        return offResult({ kind: 'PREREQUISITE_FAILED', prerequisiteKey: prereq.flag_key })
      }
      const prereqResult = evaluate(entry.flag, entry.config, ctx, store, depth + 1, nextVisited)
      if (prereqResult.variationIndex !== prereq.variation) {
        return offResult({ kind: 'PREREQUISITE_FAILED', prerequisiteKey: prereq.flag_key })
      }
    }
  }

  // STEP 3: Individual targets (always checked BEFORE rules)
  if (config.targets) {
    for (const target of config.targets) {
      if (target.context_keys && target.context_keys.includes(ctx.key)) {
        return variationResult(flag, target.variation, { kind: 'TARGET_MATCH' })
      }
    }
  }

  // STEP 4: Rules — first match wins, in stored order
  if (config.rules) {
    for (let i = 0; i < config.rules.length; i++) {
      const rule = config.rules[i]
      if (ruleMatches(rule, ctx, store)) {
        const result = resolveVariationOrRollout(flag, config, rule, ctx, {
          kind: 'RULE_MATCH',
          ruleID: rule.id,
          ruleIndex: i,
        })
        return result
      }
    }
  }

  // STEP 5: Fallthrough
  return resolveVariationOrRollout(flag, config, config.fallthrough, ctx, { kind: 'FALLTHROUGH' })
}

function variationResult(flag: Flag, idx: number, reason: Reason): EvalResult {
  if (idx < 0 || idx >= flag.variations.length) {
    return { variationIndex: null, value: null, reason: { kind: 'ERROR', errorKind: 'MALFORMED_FLAG' } }
  }
  return {
    variationIndex: idx,
    value: flag.variations[idx].value ?? null,
    reason,
  }
}

function resolveVariationOrRollout(
  flag: Flag,
  config: FlagConfig,
  vor: VariationOrRollout,
  ctx: EvalContext,
  reason: Reason,
): EvalResult {
  if (vor.variation !== undefined && vor.variation !== null) {
    return variationResult(flag, vor.variation, reason)
  }
  if (!vor.rollout || !vor.rollout.variations || vor.rollout.variations.length === 0) {
    return { variationIndex: null, value: null, reason: { kind: 'ERROR', errorKind: 'MALFORMED_FLAG' } }
  }

  return resolveRollout(flag, config, vor.rollout, ctx, reason)
}

function resolveRollout(
  flag: Flag,
  config: FlagConfig,
  rollout: Rollout,
  ctx: EvalContext,
  reason: Reason,
): EvalResult {
  const bucketBy = rollout.bucket_by || 'key'
  const bucket = computeBucket(ctx, bucketBy, flag.key, config.salt, rollout.seed)

  // Propagate experiment flag into reason
  const finalReason: Reason = rollout.is_experiment
    ? { ...reason, inExperiment: true }
    : reason

  let sum = 0.0
  for (const wv of rollout.variations) {
    sum += wv.weight / 100000.0
    if (bucket < sum) {
      return variationResult(flag, wv.variation, finalReason)
    }
  }

  // Floating-point safety net: return last variation if bucket rounds to 1.0
  const last = rollout.variations[rollout.variations.length - 1]
  return variationResult(flag, last.variation, finalReason)
}

// ruleMatches: all clauses must match (AND semantics). Empty clause list → no match.
function ruleMatches(rule: Rule, ctx: EvalContext, store: EvalStore): boolean {
  if (!rule.clauses || rule.clauses.length === 0) return false
  for (const clause of rule.clauses) {
    if (!clauseMatches(clause, ctx, store)) return false
  }
  return true
}

function clauseMatches(clause: Clause, ctx: EvalContext, store: EvalStore): boolean {
  // segmentMatch operator: check membership in named segments
  if (clause.op === 'segmentMatch') {
    for (const clauseVal of clause.values) {
      const segKey = String(clauseVal)
      const seg = store.getSegment(segKey)
      if (seg && segmentMatches(seg, ctx, store)) {
        return !clause.negate
      }
    }
    return clause.negate
  }

  // Resolve the context attribute value
  const attrVal = getAttribute(ctx, clause.attribute)

  // CRITICAL: missing attribute returns false BEFORE negation is applied.
  // "NOT (plan in [free])" must NOT match a context with no plan attribute.
  if (attrVal === undefined || attrVal === null) return false

  // Array attribute: OR semantics across elements × values
  if (Array.isArray(attrVal)) {
    for (const elem of attrVal) {
      for (const cv of clause.values) {
        if (matchSingle(clause.op, elem, cv)) {
          return !clause.negate
        }
      }
    }
    return clause.negate
  }

  // Scalar attribute: OR semantics across clause values
  for (const cv of clause.values) {
    if (matchSingle(clause.op, attrVal, cv)) {
      return !clause.negate
    }
  }
  return clause.negate
}

function getAttribute(ctx: EvalContext, name: string): unknown {
  switch (name) {
    case 'key': return ctx.key
    case 'kind': return ctx.kind ?? 'user'
    case 'anonymous': return ctx.anonymous ?? false
    default: return ctx.attributes?.[name]
  }
}

// segmentMatches: EXCLUDED always wins over INCLUDED.
function segmentMatches(seg: Segment, ctx: EvalContext, store: EvalStore): boolean {
  // Check excluded first — excluded beats included
  if (seg.excluded) {
    for (const k of seg.excluded) {
      if (k === ctx.key) return false
    }
  }
  // Check included list
  if (seg.included) {
    for (const k of seg.included) {
      if (k === ctx.key) return true
    }
  }
  // Check segment rules
  if (seg.rules) {
    for (const rule of seg.rules) {
      if (segmentRuleMatches(rule, ctx, store, seg)) return true
    }
  }
  return false
}

function segmentRuleMatches(
  rule: SegmentRule,
  ctx: EvalContext,
  store: EvalStore,
  seg: Segment,
): boolean {
  // All clauses must match
  if (!rule.clauses || rule.clauses.length === 0) return false
  for (const clause of rule.clauses) {
    if (!clauseMatches(clause, ctx, store)) return false
  }
  // Optional weight-based rollout within the segment rule
  if (rule.weight !== undefined && rule.weight !== null) {
    const bucketBy = rule.bucketBy || 'key'
    const b = computeBucket(ctx, bucketBy, seg.key, seg.salt)
    return b < rule.weight / 100000.0
  }
  return true
}
