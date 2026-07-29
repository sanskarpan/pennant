// Regex cache — compiled once per pattern, reused on every evaluation.
const regexCache = new Map<string, RegExp | null>()

function getRegex(pattern: string): RegExp | null {
  if (regexCache.has(pattern)) return regexCache.get(pattern)!
  try {
    const re = new RegExp(pattern)
    regexCache.set(pattern, re)
    return re
  } catch {
    regexCache.set(pattern, null)
    return null
  }
}

// parseSemVer parses a semver string like "1.2.3" (or "1.2.3-pre") into [major, minor, patch].
// Matches Go's Masterminds/semver behavior for the three-part numeric comparison.
function parseSemVer(s: string): [number, number, number] | null {
  // Allow optional leading "v" and optional pre-release/build metadata after patch
  const m = /^v?(\d+)\.(\d+)\.(\d+)/.exec(s)
  if (!m) return null
  return [parseInt(m[1], 10), parseInt(m[2], 10), parseInt(m[3], 10)]
}

function compareSemVer(a: string, b: string): number | null {
  const av = parseSemVer(a)
  const bv = parseSemVer(b)
  if (!av || !bv) return null
  for (let i = 0; i < 3; i++) {
    const d = av[i] - bv[i]
    if (d !== 0) return d < 0 ? -1 : 1
  }
  return 0
}

// toFloat64 mirrors Go's toFloat64: accepts number, or a string parseable as float.
function toFloat64(v: unknown): number | null {
  if (typeof v === 'number') return v
  if (typeof v === 'string') {
    const f = parseFloat(v)
    return isNaN(f) ? null : f
  }
  return null
}

// parseTime mirrors Go's parseTime: accepts RFC 3339 string or Unix millisecond timestamp.
function parseTime(v: unknown): number | null {
  if (typeof v === 'string') {
    const t = Date.parse(v)
    return isNaN(t) ? null : t
  }
  if (typeof v === 'number') {
    // treat as Unix milliseconds (Go: time.UnixMilli(int64(t)))
    return v
  }
  return null
}

// matchSingle: does a single context attribute value match a single clause value?
// clauseVal is already a parsed JS value (not raw JSON) by the time it arrives here.
export function matchSingle(
  op: string,
  ctxVal: unknown,
  clauseVal: unknown,
): boolean {
  switch (op) {
    case 'in': {
      // Mirrors Go jsonValuesEqual: string==string, number==number, bool==bool, null==null
      if (typeof ctxVal === 'string' && typeof clauseVal === 'string') return ctxVal === clauseVal
      if (typeof ctxVal === 'number' && typeof clauseVal === 'number') return ctxVal === clauseVal
      if (typeof ctxVal === 'boolean' && typeof clauseVal === 'boolean') return ctxVal === clauseVal
      if (ctxVal === null && clauseVal === null) return true
      return false
    }

    case 'endsWith':
      return typeof ctxVal === 'string' && typeof clauseVal === 'string'
        ? ctxVal.endsWith(clauseVal)
        : false

    case 'startsWith':
      return typeof ctxVal === 'string' && typeof clauseVal === 'string'
        ? ctxVal.startsWith(clauseVal)
        : false

    case 'contains':
      return typeof ctxVal === 'string' && typeof clauseVal === 'string'
        ? ctxVal.includes(clauseVal)
        : false

    case 'matches': {
      if (typeof ctxVal !== 'string' || typeof clauseVal !== 'string') return false
      const re = getRegex(clauseVal)
      return re !== null ? re.test(ctxVal) : false
    }

    case 'lessThan': {
      const a = toFloat64(ctxVal), b = toFloat64(clauseVal)
      return a !== null && b !== null && a < b
    }

    case 'lessThanOrEqual': {
      const a = toFloat64(ctxVal), b = toFloat64(clauseVal)
      return a !== null && b !== null && a <= b
    }

    case 'greaterThan': {
      const a = toFloat64(ctxVal), b = toFloat64(clauseVal)
      return a !== null && b !== null && a > b
    }

    case 'greaterThanOrEqual': {
      const a = toFloat64(ctxVal), b = toFloat64(clauseVal)
      return a !== null && b !== null && a >= b
    }

    case 'before': {
      const a = parseTime(ctxVal), b = parseTime(clauseVal)
      return a !== null && b !== null && a < b
    }

    case 'after': {
      const a = parseTime(ctxVal), b = parseTime(clauseVal)
      return a !== null && b !== null && a > b
    }

    case 'semVerEqual': {
      if (typeof ctxVal !== 'string' || typeof clauseVal !== 'string') return false
      const d = compareSemVer(ctxVal, clauseVal)
      return d === 0
    }

    case 'semVerLessThan': {
      if (typeof ctxVal !== 'string' || typeof clauseVal !== 'string') return false
      const d = compareSemVer(ctxVal, clauseVal)
      return d !== null && d < 0
    }

    case 'semVerGreaterThan': {
      if (typeof ctxVal !== 'string' || typeof clauseVal !== 'string') return false
      const d = compareSemVer(ctxVal, clauseVal)
      return d !== null && d > 0
    }

    default:
      return false
  }
}
