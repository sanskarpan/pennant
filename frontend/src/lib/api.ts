import { authStorage, refreshToken } from './auth'

const BASE = '/api/v1'

export interface Flag {
  key: string
  name: string
  description?: string
  type: 'boolean' | 'string' | 'number' | 'json'
  variations: Array<{ id: string; value: unknown; name?: string }>
  tags?: string[]
  temporary?: boolean
  archived?: boolean
  createdAt?: string
  updatedAt?: string
}

export interface FlagConfig {
  on: boolean
  offVariation: number | null
  prerequisites: Array<{ key: string; variation: number }>
  targets: Array<{ values: string[]; variation: number }>
  rules: Array<{
    id: string
    clauses: Array<{
      attribute: string
      op: string
      values: unknown[]
      negate: boolean
    }>
    variation?: number
    rollout?: {
      variations: Array<{ variation: number; weight: number }>
      bucketBy: string
    }
  }>
  fallthrough: { variation?: number; rollout?: unknown }
  salt: string
}

export interface Project {
  key: string
  name: string
  description?: string
  environments?: Array<{ key: string; name: string }>
}

async function request<T>(url: string, opts?: RequestInit): Promise<T> {
  const token = authStorage.getToken()
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(opts?.headers as Record<string, string>),
  }
  if (token) headers['Authorization'] = `Bearer ${token}`

  let res = await fetch(url, { ...opts, headers })

  // Token expired → try refresh once
  if (res.status === 401) {
    const refreshed = await refreshToken()
    if (refreshed) {
      const newToken = authStorage.getToken()
      if (newToken) headers['Authorization'] = `Bearer ${newToken}`
      res = await fetch(url, { ...opts, headers })
    }
    if (res.status === 401) {
      authStorage.clearTokens()
      window.location.reload()
      throw new Error('Session expired')
    }
  }

  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error || `HTTP ${res.status}`)
  }
  if (res.status === 204) return null as T
  return res.json()
}

export const api = {
  listProjects: () => request<Project[]>(`${BASE}/projects`),

  listFlags: (projectKey: string) =>
    request<Flag[]>(`${BASE}/projects/${projectKey}/flags`),

  createFlag: (projectKey: string, flag: Partial<Flag>) =>
    request<Flag>(`${BASE}/projects/${projectKey}/flags`, {
      method: 'POST',
      body: JSON.stringify(flag),
    }),

  updateFlag: (projectKey: string, flag: Flag) =>
    request<Flag>(`${BASE}/projects/${projectKey}/flags/${flag.key}`, {
      method: 'PUT',
      body: JSON.stringify(flag),
    }),

  deleteFlag: (projectKey: string, flagKey: string) =>
    request<null>(`${BASE}/projects/${projectKey}/flags/${flagKey}`, {
      method: 'DELETE',
    }),

  getFlagConfig: (projectKey: string, flagKey: string, envKey: string) =>
    request<FlagConfig>(
      `${BASE}/projects/${projectKey}/flags/${flagKey}/environments/${envKey}`
    ),

  putFlagConfig: (projectKey: string, flagKey: string, envKey: string, cfg: FlagConfig) =>
    request<FlagConfig>(
      `${BASE}/projects/${projectKey}/flags/${flagKey}/environments/${envKey}`,
      { method: 'PUT', body: JSON.stringify(cfg) }
    ),

  listEnvironments: (projectKey: string) =>
    request<Array<{ key: string; name: string }>>(`${BASE}/projects/${projectKey}/environments`).catch(() => [] as Array<{ key: string; name: string }>),

  listSegments: (projectKey: string) =>
    request<Array<{ key: string; name: string }>>(`${BASE}/projects/${projectKey}/segments`),

  // Experiments
  listExperiments: (projectKey: string) =>
    request<Experiment[]>(`${BASE}/projects/${projectKey}/experiments`).catch(() => [] as Experiment[]),

  getExperimentResults: (projectKey: string, expKey: string) =>
    request<ExperimentResults>(`${BASE}/projects/${projectKey}/experiments/${expKey}/results`).catch(() => null),

  createExperiment: (projectKey: string, exp: Partial<Experiment>) =>
    request<Experiment>(`${BASE}/projects/${projectKey}/experiments`, {
      method: 'POST',
      body: JSON.stringify(exp),
    }),

  // Segments (full CRUD)
  listSegmentsApi: (projectKey: string) =>
    request<Segment[]>(`${BASE}/projects/${projectKey}/segments`).catch(() => [] as Segment[]),

  createSegment: (projectKey: string, seg: Partial<Segment>) =>
    request<Segment>(`${BASE}/projects/${projectKey}/segments`, {
      method: 'POST',
      body: JSON.stringify(seg),
    }),

  updateSegment: (projectKey: string, seg: Segment) =>
    request<Segment>(`${BASE}/projects/${projectKey}/segments/${seg.key}`, {
      method: 'PUT',
      body: JSON.stringify(seg),
    }),

  deleteSegment: (projectKey: string, segKey: string) =>
    request<null>(`${BASE}/projects/${projectKey}/segments/${segKey}`, {
      method: 'DELETE',
    }),
}

// ─── Experiment types ───────────────────────────────────────────────────────

export interface ExperimentVariation {
  variationIndex: number
  name: string
  isControl: boolean
  weight: number
}

export interface Metric {
  key: string
  name: string
  eventName: string
  kind: 'conversion' | 'revenue' | 'count'
  isGuardrail: boolean
  mde: number
}

export interface Experiment {
  key: string
  name: string
  description?: string
  flagKey: string
  environmentKey: string
  projectKey: string
  variations: ExperimentVariation[]
  metrics: Metric[]
  status: 'draft' | 'running' | 'paused' | 'stopped' | 'archived'
  alpha: number
  power: number
  startedAt?: string
  stoppedAt?: string
  createdAt: string
}

export interface VariantMetric {
  variationIndex: number
  name: string
  controlN: number
  controlConv: number
  treatmentN: number
  treatmentConv: number
  controlRate: number
  treatmentRate: number
  absoluteEffect: number
  relativeEffect: number
  zScore: number
  pValue: number
  ciLower: number
  ciUpper: number
  significant: boolean
  alwaysValidPValue: number
  seqDecision: 'continue' | 'reject_null' | 'accept_null'
  samplesRequired: number
}

export interface MetricResult {
  metricKey: string
  metricName: string
  variants: VariantMetric[]
}

export interface ExperimentResults {
  experimentKey: string
  computedAt: string
  totalSamples: number
  srm: {
    chiSquare: number
    pValue: number
    mismatch: boolean
  }
  metricResults: MetricResult[]
}

// ─── Segment types ──────────────────────────────────────────────────────────

export interface Segment {
  key: string
  included: string[]
  excluded: string[]
  rules: Array<{
    id: string
    clauses: Array<{
      attribute: string
      op: string
      values: unknown[]
      negate: boolean
    }>
    weight?: number
    bucketBy?: string
  }>
  salt: string
}
