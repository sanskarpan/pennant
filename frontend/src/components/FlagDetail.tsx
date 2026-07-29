import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type FlagConfig, type Flag } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { useState, useEffect } from 'react'
import { Plus, Trash2, GripVertical, Save } from 'lucide-react'

interface FlagDetailProps {
  flag: Flag
  projectKey: string
  envKey: string
}

const OPERATORS = [
  { value: 'in', label: 'is one of' },
  { value: 'notIn', label: 'is not one of' },
  { value: 'startsWith', label: 'starts with' },
  { value: 'endsWith', label: 'ends with' },
  { value: 'contains', label: 'contains' },
  { value: 'matches', label: 'matches regex' },
  { value: 'lessThan', label: '< (less than)' },
  { value: 'lessThanOrEqual', label: '<= (≤)' },
  { value: 'greaterThan', label: '> (greater than)' },
  { value: 'greaterThanOrEqual', label: '>= (≥)' },
  { value: 'before', label: 'before (date)' },
  { value: 'after', label: 'after (date)' },
  { value: 'semVerEqual', label: 'semver =' },
  { value: 'semVerLessThan', label: 'semver <' },
  { value: 'semVerGreaterThan', label: 'semver >' },
]

export function FlagDetail({ flag, projectKey, envKey }: FlagDetailProps) {
  const qc = useQueryClient()

  const { data: config, isLoading } = useQuery({
    queryKey: ['flagConfig', projectKey, flag.key, envKey],
    queryFn: () => api.getFlagConfig(projectKey, flag.key, envKey),
  })

  const [draft, setDraft] = useState<FlagConfig | null>(null)
  const [dirty, setDirty] = useState(false)

  useEffect(() => {
    if (config && !dirty) {
      setDraft(config)
    }
  }, [config, dirty])

  const saveMutation = useMutation({
    mutationFn: () => {
      if (!draft) throw new Error('no draft')
      return api.putFlagConfig(projectKey, flag.key, envKey, draft)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['flagConfig', projectKey, flag.key, envKey] })
      setDirty(false)
    },
  })

  const update = (patch: Partial<FlagConfig>) => {
    setDraft(d => d ? { ...d, ...patch } : null)
    setDirty(true)
  }

  if (isLoading || !draft) {
    return <div className="p-6 text-muted-foreground">Loading targeting…</div>
  }

  return (
    <div className="space-y-6 p-6 max-w-3xl">
      {/* Header */}
      <div className="flex items-start justify-between">
        <div>
          <h2 className="text-xl font-semibold">{flag.name}</h2>
          <p className="text-sm font-mono text-muted-foreground mt-0.5">{flag.key}</p>
        </div>
        <div className="flex gap-2 items-center">
          <Badge variant={draft.on ? 'success' : 'secondary'}>
            {draft.on ? 'ON' : 'OFF'}
          </Badge>
          <button
            onClick={() => update({ on: !draft.on })}
            className="text-sm text-muted-foreground hover:text-foreground underline"
          >
            toggle
          </button>
        </div>
      </div>

      {/* Variations */}
      <section>
        <h3 className="text-sm font-medium mb-2">Variations</h3>
        <div className="grid gap-1">
          {flag.variations.map((v, i) => (
            <div key={v.id} className="flex items-center gap-3 rounded border border-border px-3 py-2 text-sm">
              <span className="w-5 text-muted-foreground text-right">{i}</span>
              <code className="font-mono flex-1">{JSON.stringify(v.value)}</code>
              {v.name && <span className="text-muted-foreground">{v.name}</span>}
            </div>
          ))}
        </div>
      </section>

      {/* Individual Targets */}
      <section>
        <div className="flex items-center justify-between mb-2">
          <h3 className="text-sm font-medium">Individual Targets</h3>
          <Button size="sm" variant="outline" onClick={() => {
            update({
              targets: [...(draft.targets || []), { values: [], variation: 0 }]
            })
          }}>
            <Plus className="h-3 w-3 mr-1" /> Add target
          </Button>
        </div>
        {(draft.targets || []).length === 0 && (
          <p className="text-sm text-muted-foreground">No individual targets. Rules apply to all users.</p>
        )}
        {(draft.targets || []).map((target, i) => (
          <div key={i} className="flex gap-2 mb-2 items-start">
            <div className="flex-1 space-y-1">
              <input
                className="w-full rounded border border-input bg-background px-2 py-1 text-sm font-mono"
                placeholder="user-key-1, user-key-2, ..."
                value={target.values.join(', ')}
                onChange={e => {
                  const newTargets = [...draft.targets]
                  newTargets[i] = {
                    ...target,
                    values: e.target.value.split(',').map(v => v.trim()).filter(Boolean)
                  }
                  update({ targets: newTargets })
                }}
              />
            </div>
            <select
              className="rounded border border-input bg-background px-2 py-1 text-sm"
              value={target.variation}
              onChange={e => {
                const newTargets = [...draft.targets]
                newTargets[i] = { ...target, variation: Number(e.target.value) }
                update({ targets: newTargets })
              }}
            >
              {flag.variations.map((v, vi) => (
                <option key={v.id} value={vi}>
                  {vi}: {v.name || JSON.stringify(v.value)}
                </option>
              ))}
            </select>
            <Button size="icon" variant="ghost" onClick={() => {
              update({ targets: draft.targets.filter((_, j) => j !== i) })
            }}>
              <Trash2 className="h-4 w-4" />
            </Button>
          </div>
        ))}
      </section>

      {/* Rules */}
      <section>
        <div className="flex items-center justify-between mb-2">
          <h3 className="text-sm font-medium">Targeting Rules</h3>
          <Button size="sm" variant="outline" onClick={() => {
            const newRule = {
              id: `rule-${Date.now()}`,
              clauses: [{ attribute: 'key', op: 'in', values: [], negate: false }],
              variation: flag.variations.length > 1 ? 1 : 0,
            }
            update({ rules: [...(draft.rules || []), newRule] })
          }}>
            <Plus className="h-3 w-3 mr-1" /> Add rule
          </Button>
        </div>

        {(draft.rules || []).length === 0 && (
          <p className="text-sm text-muted-foreground">No rules. All users get the fallthrough variation.</p>
        )}

        {(draft.rules || []).map((rule, ri) => (
          <div key={rule.id} className="mb-3 rounded-lg border border-border p-3 space-y-2">
            <div className="flex items-center gap-2">
              <GripVertical className="h-4 w-4 text-muted-foreground" />
              <span className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Rule {ri + 1}</span>
              <div className="flex-1" />
              <select
                className="text-sm rounded border border-input bg-background px-2 py-1"
                value={rule.variation ?? 0}
                onChange={e => {
                  const newRules = [...draft.rules]
                  newRules[ri] = { ...rule, variation: Number(e.target.value) }
                  update({ rules: newRules })
                }}
              >
                {flag.variations.map((v, vi) => (
                  <option key={v.id} value={vi}>serve {vi}: {v.name || JSON.stringify(v.value)}</option>
                ))}
              </select>
              <Button size="icon" variant="ghost" onClick={() => {
                update({ rules: draft.rules.filter((_, j) => j !== ri) })
              }}>
                <Trash2 className="h-4 w-4" />
              </Button>
            </div>

            {/* Clauses */}
            {rule.clauses.map((clause, ci) => (
              <div key={ci} className="flex gap-2 items-center pl-6">
                <input
                  className="w-32 rounded border border-input bg-background px-2 py-1 text-xs font-mono"
                  placeholder="attribute"
                  value={clause.attribute}
                  onChange={e => {
                    const newRules = [...draft.rules]
                    const newClauses = [...rule.clauses]
                    newClauses[ci] = { ...clause, attribute: e.target.value }
                    newRules[ri] = { ...rule, clauses: newClauses }
                    update({ rules: newRules })
                  }}
                />
                <select
                  className="rounded border border-input bg-background px-2 py-1 text-xs"
                  value={clause.op}
                  onChange={e => {
                    const newRules = [...draft.rules]
                    const newClauses = [...rule.clauses]
                    newClauses[ci] = { ...clause, op: e.target.value }
                    newRules[ri] = { ...rule, clauses: newClauses }
                    update({ rules: newRules })
                  }}
                >
                  {OPERATORS.map(op => (
                    <option key={op.value} value={op.value}>{op.label}</option>
                  ))}
                </select>
                <input
                  className="flex-1 rounded border border-input bg-background px-2 py-1 text-xs font-mono"
                  placeholder="value1, value2"
                  value={(clause.values as string[]).join(', ')}
                  onChange={e => {
                    const newRules = [...draft.rules]
                    const newClauses = [...rule.clauses]
                    newClauses[ci] = {
                      ...clause,
                      values: e.target.value.split(',').map(v => v.trim()).filter(Boolean)
                    }
                    newRules[ri] = { ...rule, clauses: newClauses }
                    update({ rules: newRules })
                  }}
                />
                <label className="flex items-center gap-1 text-xs">
                  <input
                    type="checkbox"
                    checked={clause.negate}
                    onChange={e => {
                      const newRules = [...draft.rules]
                      const newClauses = [...rule.clauses]
                      newClauses[ci] = { ...clause, negate: e.target.checked }
                      newRules[ri] = { ...rule, clauses: newClauses }
                      update({ rules: newRules })
                    }}
                  />
                  NOT
                </label>
                <Button size="icon" variant="ghost" className="h-6 w-6" onClick={() => {
                  const newRules = [...draft.rules]
                  newRules[ri] = { ...rule, clauses: rule.clauses.filter((_, j) => j !== ci) }
                  update({ rules: newRules })
                }}>
                  <Trash2 className="h-3 w-3" />
                </Button>
              </div>
            ))}

            <Button size="sm" variant="ghost" className="ml-6 text-xs" onClick={() => {
              const newRules = [...draft.rules]
              newRules[ri] = {
                ...rule,
                clauses: [...rule.clauses, { attribute: '', op: 'in', values: [], negate: false }]
              }
              update({ rules: newRules })
            }}>
              <Plus className="h-3 w-3 mr-1" /> AND clause
            </Button>
          </div>
        ))}
      </section>

      {/* Fallthrough */}
      <section>
        <h3 className="text-sm font-medium mb-2">Default Rule (fallthrough)</h3>
        <div className="flex items-center gap-3">
          <span className="text-sm text-muted-foreground">Serve</span>
          <select
            className="rounded border border-input bg-background px-2 py-1 text-sm"
            value={draft.fallthrough?.variation ?? 0}
            onChange={e => {
              update({ fallthrough: { variation: Number(e.target.value) } })
            }}
          >
            {flag.variations.map((v, vi) => (
              <option key={v.id} value={vi}>
                {vi}: {v.name || JSON.stringify(v.value)}
              </option>
            ))}
          </select>
          <span className="text-sm text-muted-foreground">to all remaining users</span>
        </div>
      </section>

      {/* Save */}
      {dirty && (
        <div className="flex items-center gap-3 pt-2 border-t border-border">
          <Button onClick={() => saveMutation.mutate()} disabled={saveMutation.isPending}>
            <Save className="h-4 w-4 mr-2" />
            {saveMutation.isPending ? 'Saving…' : 'Save targeting rules'}
          </Button>
          <Button variant="ghost" onClick={() => { setDraft(config!); setDirty(false) }}>
            Discard changes
          </Button>
          {saveMutation.isError && (
            <span className="text-sm text-destructive">
              {(saveMutation.error as Error)?.message}
            </span>
          )}
        </div>
      )}
    </div>
  )
}
