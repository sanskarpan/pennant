export type VariationType = 'boolean' | 'string' | 'number' | 'json'

export interface Variation {
  id: string
  value: unknown
  name?: string
  description?: string
}

export interface WeightedVariation {
  variation: number
  weight: number // 0-100000
}

export interface Rollout {
  variations: WeightedVariation[]
  bucket_by: string
  seed?: number
  is_experiment?: boolean
}

export interface VariationOrRollout {
  variation?: number
  rollout?: Rollout
}

export type Operator =
  | 'in'
  | 'endsWith'
  | 'startsWith'
  | 'matches'
  | 'contains'
  | 'lessThan'
  | 'lessThanOrEqual'
  | 'greaterThan'
  | 'greaterThanOrEqual'
  | 'before'
  | 'after'
  | 'semVerEqual'
  | 'semVerLessThan'
  | 'semVerGreaterThan'
  | 'segmentMatch'

export interface Clause {
  attribute: string
  op: Operator
  values: unknown[]
  negate: boolean
}

export interface Rule {
  id: string
  clauses: Clause[]
  variation?: number
  rollout?: Rollout
}

export interface Target {
  context_keys: string[]
  variation: number
}

export interface Prerequisite {
  flag_key: string
  variation: number
}

export interface FlagConfig {
  on: boolean
  off_variation: number | null
  prerequisites: Prerequisite[]
  targets: Target[]
  rules: Rule[]
  fallthrough: VariationOrRollout
  salt: string
}

export interface Flag {
  key: string
  name: string
  description?: string
  type: VariationType
  variations: Variation[]
  tags?: string[]
  temporary?: boolean
  archived?: boolean
}

export interface Segment {
  key: string
  included: string[]
  excluded: string[]
  rules: SegmentRule[]
  salt: string
}

export interface SegmentRule {
  id: string
  clauses: Clause[]
  weight?: number
  bucketBy?: string
}

export interface EvalContext {
  key: string
  kind?: string
  anonymous?: boolean
  attributes?: Record<string, unknown>
}

export type ReasonKind =
  | 'OFF'
  | 'FALLTHROUGH'
  | 'TARGET_MATCH'
  | 'RULE_MATCH'
  | 'PREREQUISITE_FAILED'
  | 'ERROR'

export type ErrorKind =
  | 'FLAG_NOT_FOUND'
  | 'MALFORMED_FLAG'
  | 'WRONG_TYPE'
  | 'CLIENT_NOT_READY'
  | 'PREREQUISITE_CYCLE'

export interface Reason {
  kind: ReasonKind
  ruleID?: string
  ruleIndex?: number
  prerequisiteKey?: string
  errorKind?: ErrorKind
  inExperiment?: boolean
}

export interface EvalResult {
  variationIndex: number | null
  value: unknown
  reason: Reason
}

export interface FlagEntry {
  flag: Flag
  config: FlagConfig
}

export interface Snapshot {
  environmentKey: string
  projectKey: string
  version: number
  flags: Record<string, ResolvedFlag>
  segments: Record<string, Segment>
  checksum: string
  publishedAt: string
}

export interface ResolvedFlag {
  key: string
  name: string
  description?: string
  type: VariationType
  variations: Variation[]
  tags?: string[]
  temporary?: boolean
  archived?: boolean
  config: FlagConfig
}
