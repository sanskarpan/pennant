import { createHash } from 'crypto'
import type { EvalContext } from './types.js'

// BucketScale is 2^60 - 1 (15 hex Fs = 0xFFFFFFFFFFFFFFF).
// We MUST use BigInt here because 60-bit integers exceed Number.MAX_SAFE_INTEGER
// (2^53 - 1 = 9007199254740991). Using a plain JS number would silently lose
// precision and produce different bucket values from the Go reference implementation.
const BUCKET_SCALE = BigInt('0xFFFFFFFFFFFFFFF')
// Pre-compute the divisor as a number for the final division step.
// Number(BUCKET_SCALE) is exact because floats can represent this integer precisely
// when used only as the denominator — the loss of precision in integer representation
// does not affect the ratio when the numerator is also subject to the same conversion.
const BUCKET_SCALE_NUM = Number(BUCKET_SCALE)

// computeBucket returns a deterministic float in [0.0, 1.0) for the given context.
//
// Hash input format (MUST match Go implementation exactly):
//   - Without seed: "{flagKey}.{salt}.{idStr}"
//   - With seed:    "{seed}.{idStr}"
//
// We take the first 15 hex characters of the SHA-1 digest (= 60 bits),
// parse them as a big integer, and divide by BucketScale.
export function computeBucket(
  ctx: EvalContext,
  bucketBy: string,
  flagKey: string,
  salt: string,
  seed?: number,
): number {
  if (!bucketBy) bucketBy = 'key'

  const attrVal = getAttribute(ctx, bucketBy)
  if (attrVal === undefined || attrVal === null) return 0.0

  const strVal = stringifyBucketValue(attrVal)
  if (strVal === null) return 0.0

  // Hash input must match Go fmt.Sprintf exactly.
  let input: string
  if (seed !== undefined && seed !== null) {
    // With seed: "{seed}.{idStr}"
    input = `${seed}.${strVal}`
  } else {
    // Without seed: "{flagKey}.{salt}.{idStr}"
    input = `${flagKey}.${salt}.${strVal}`
  }

  const hash = createHash('sha1').update(input, 'utf8').digest('hex')

  // Take the first 15 hex characters = 60 bits
  const prefix = hash.slice(0, 15)
  const big = BigInt(`0x${prefix}`)

  return Number(big) / BUCKET_SCALE_NUM
}

function getAttribute(ctx: EvalContext, name: string): unknown {
  switch (name) {
    case 'key': return ctx.key
    case 'kind': return ctx.kind ?? 'user'
    case 'anonymous': return ctx.anonymous ?? false
    default: return ctx.attributes?.[name]
  }
}

// stringifyBucketValue converts an attribute value to the string used for hashing.
// ONLY strings and integral numbers are valid. Non-integral floats return null.
// This matches the Go stringifyBucketValue function exactly.
function stringifyBucketValue(v: unknown): string | null {
  if (typeof v === 'string') return v
  if (typeof v === 'number') {
    // Integral floats are allowed (JS numbers are all floats).
    // Match Go's condition: t == math.Trunc(t) && math.Abs(t) < 1e15
    if (Number.isInteger(v) && Math.abs(v) < 1e15) {
      return String(Math.trunc(v))
    }
    return null
  }
  return null
}
