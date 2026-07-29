import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type Flag } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Trash2, Plus, ToggleLeft, ToggleRight } from 'lucide-react'
import { useState } from 'react'

interface FlagListProps {
  projectKey: string
  envKey: string
  onSelectFlag: (flag: Flag) => void
}

export function FlagList({ projectKey, envKey, onSelectFlag }: FlagListProps) {
  const qc = useQueryClient()
  const [creating, setCreating] = useState(false)
  const [newFlagKey, setNewFlagKey] = useState('')
  const [newFlagName, setNewFlagName] = useState('')

  const { data: flags = [], isLoading } = useQuery({
    queryKey: ['flags', projectKey],
    queryFn: () => api.listFlags(projectKey),
  })

  const deleteMutation = useMutation({
    mutationFn: (flagKey: string) => api.deleteFlag(projectKey, flagKey),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['flags', projectKey] }),
  })

  const createMutation = useMutation({
    mutationFn: (flag: Partial<Flag>) => api.createFlag(projectKey, flag),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['flags', projectKey] })
      setCreating(false)
      setNewFlagKey('')
      setNewFlagName('')
    },
  })

  const handleCreate = () => {
    if (!newFlagKey.trim()) return
    createMutation.mutate({
      key: newFlagKey.trim(),
      name: newFlagName.trim() || newFlagKey.trim(),
      type: 'boolean',
      variations: [
        { id: 'v0', value: false, name: 'false' },
        { id: 'v1', value: true, name: 'true' },
      ],
    })
  }

  if (isLoading) {
    return <div className="p-4 text-muted-foreground">Loading flags…</div>
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between px-4 py-2">
        <h2 className="text-sm font-semibold text-foreground">Feature Flags</h2>
        <Button size="sm" variant="outline" onClick={() => setCreating(true)}>
          <Plus className="mr-1 h-3 w-3" /> New Flag
        </Button>
      </div>

      {creating && (
        <div className="mx-4 rounded-lg border border-dashed border-border p-3 space-y-2">
          <input
            className="w-full rounded border border-input bg-background px-2 py-1 text-sm"
            placeholder="flag-key (snake-case)"
            value={newFlagKey}
            onChange={e => setNewFlagKey(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && handleCreate()}
            autoFocus
          />
          <input
            className="w-full rounded border border-input bg-background px-2 py-1 text-sm"
            placeholder="Display name (optional)"
            value={newFlagName}
            onChange={e => setNewFlagName(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && handleCreate()}
          />
          <div className="flex gap-2">
            <Button size="sm" onClick={handleCreate} disabled={createMutation.isPending}>
              {createMutation.isPending ? 'Creating…' : 'Create'}
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setCreating(false)}>Cancel</Button>
          </div>
        </div>
      )}

      <ul className="space-y-1 px-2">
        {flags.map(flag => (
          <FlagRow
            key={flag.key}
            flag={flag}
            projectKey={projectKey}
            envKey={envKey}
            onSelect={() => onSelectFlag(flag)}
            onDelete={() => {
              if (confirm(`Delete flag "${flag.key}"?`)) {
                deleteMutation.mutate(flag.key)
              }
            }}
          />
        ))}
        {flags.length === 0 && !creating && (
          <li className="px-2 py-4 text-center text-sm text-muted-foreground">
            No flags yet. Create one to get started.
          </li>
        )}
      </ul>
    </div>
  )
}

function FlagRow({
  flag,
  projectKey,
  envKey,
  onSelect,
  onDelete,
}: {
  flag: Flag
  projectKey: string
  envKey: string
  onSelect: () => void
  onDelete: () => void
}) {
  const qc = useQueryClient()
  const { data: config } = useQuery({
    queryKey: ['flagConfig', projectKey, flag.key, envKey],
    queryFn: () => api.getFlagConfig(projectKey, flag.key, envKey),
  })

  const toggleMutation = useMutation({
    mutationFn: () => {
      if (!config) throw new Error('no config')
      return api.putFlagConfig(projectKey, flag.key, envKey, {
        ...config,
        on: !config.on,
      })
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['flagConfig', projectKey, flag.key, envKey] })
    },
  })

  const isOn = config?.on ?? false

  return (
    <li className="group flex items-center gap-2 rounded-md px-2 py-2 hover:bg-accent/50 cursor-pointer">
      <button
        onClick={e => { e.stopPropagation(); toggleMutation.mutate() }}
        className="shrink-0 text-muted-foreground hover:text-foreground"
        title={isOn ? 'Turn off' : 'Turn on'}
      >
        {isOn
          ? <ToggleRight className="h-5 w-5 text-green-500" />
          : <ToggleLeft className="h-5 w-5" />}
      </button>

      <div className="flex-1 min-w-0" onClick={onSelect}>
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium truncate">{flag.name}</span>
          <Badge variant="outline" className="shrink-0 text-xs">{flag.type}</Badge>
        </div>
        <span className="text-xs text-muted-foreground font-mono">{flag.key}</span>
      </div>

      <button
        onClick={e => { e.stopPropagation(); onDelete() }}
        className="shrink-0 opacity-0 group-hover:opacity-100 text-muted-foreground hover:text-destructive"
      >
        <Trash2 className="h-4 w-4" />
      </button>
    </li>
  )
}
