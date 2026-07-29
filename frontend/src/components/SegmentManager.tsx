import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { Segment } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useState } from 'react'
import { Plus, Trash2, Save, Users } from 'lucide-react'

interface SegmentManagerProps {
  projectKey: string
}

export function SegmentManager({ projectKey }: SegmentManagerProps) {
  const qc = useQueryClient()
  const [selectedKey, setSelectedKey] = useState<string | null>(null)
  const [creating, setCreating] = useState(false)
  const [newKey, setNewKey] = useState('')

  const { data: segments = [] } = useQuery({
    queryKey: ['segments', projectKey],
    queryFn: () => api.listSegmentsApi(projectKey),
  })

  const createMutation = useMutation({
    mutationFn: () =>
      api.createSegment(projectKey, {
        key: newKey.trim(),
        included: [],
        excluded: [],
        rules: [],
        salt: newKey.trim(),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['segments', projectKey] })
      setCreating(false)
      setNewKey('')
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (key: string) => api.deleteSegment(projectKey, key),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['segments', projectKey] })
      setSelectedKey(null)
    },
  })

  const selected = segments.find((s) => s.key === selectedKey)

  return (
    <div className="flex gap-4 h-full">
      {/* Segment list */}
      <div className="w-64 shrink-0 space-y-2">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-medium">Segments</h3>
          <Button
            size="sm"
            variant="outline"
            onClick={() => setCreating(true)}
          >
            <Plus className="h-3 w-3" />
          </Button>
        </div>

        {creating && (
          <div className="space-y-2 rounded-lg border border-dashed border-border p-2">
            <input
              className="w-full rounded border border-input bg-background px-2 py-1 text-sm font-mono"
              placeholder="segment-key"
              value={newKey}
              onChange={(e) => setNewKey(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && createMutation.mutate()}
              autoFocus
            />
            <div className="flex gap-1">
              <Button
                size="sm"
                onClick={() => createMutation.mutate()}
                disabled={!newKey.trim()}
              >
                Create
              </Button>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => setCreating(false)}
              >
                Cancel
              </Button>
            </div>
          </div>
        )}

        <ul className="space-y-1">
          {segments.map((seg) => (
            <li
              key={seg.key}
              onClick={() => setSelectedKey(seg.key)}
              className={`group flex items-center gap-2 rounded-md px-2 py-2 cursor-pointer text-sm ${
                selectedKey === seg.key ? 'bg-accent' : 'hover:bg-accent/50'
              }`}
            >
              <Users className="h-4 w-4 text-muted-foreground shrink-0" />
              <span className="flex-1 truncate font-mono">{seg.key}</span>
              <button
                onClick={(e) => {
                  e.stopPropagation()
                  deleteMutation.mutate(seg.key)
                }}
                className="opacity-0 group-hover:opacity-100 text-muted-foreground hover:text-destructive"
              >
                <Trash2 className="h-3 w-3" />
              </button>
            </li>
          ))}
          {segments.length === 0 && !creating && (
            <li className="text-sm text-muted-foreground py-4 text-center">
              No segments yet
            </li>
          )}
        </ul>
      </div>

      {/* Segment editor */}
      <div className="flex-1">
        {selected ? (
          <SegmentEditor
            segment={selected}
            projectKey={projectKey}
            onSaved={() =>
              qc.invalidateQueries({ queryKey: ['segments', projectKey] })
            }
          />
        ) : (
          <div className="text-center text-muted-foreground py-12 text-sm">
            Select a segment to edit its membership rules
          </div>
        )}
      </div>
    </div>
  )
}

function SegmentEditor({
  segment,
  projectKey,
  onSaved,
}: {
  segment: Segment
  projectKey: string
  onSaved: () => void
}) {
  const [draft, setDraft] = useState<Segment>({ ...segment })
  const [dirty, setDirty] = useState(false)

  const update = (patch: Partial<Segment>) => {
    setDraft((d) => ({ ...d, ...patch }))
    setDirty(true)
  }

  const saveMutation = useMutation({
    mutationFn: () => api.updateSegment(projectKey, draft),
    onSuccess: () => {
      setDirty(false)
      onSaved()
    },
  })

  return (
    <div className="space-y-6">
      <h3 className="text-base font-semibold font-mono">{segment.key}</h3>

      {/* Included users */}
      <section>
        <h4 className="text-sm font-medium mb-2">Included users (by key)</h4>
        <textarea
          className="w-full rounded border border-input bg-background px-3 py-2 text-sm font-mono resize-y min-h-16"
          placeholder={'user-key-1\nuser-key-2\nuser-key-3'}
          value={draft.included.join('\n')}
          onChange={(e) =>
            update({
              included: e.target.value
                .split('\n')
                .map((v) => v.trim())
                .filter(Boolean),
            })
          }
        />
        <p className="text-xs text-muted-foreground mt-1">
          {draft.included.length} users included
        </p>
      </section>

      {/* Excluded users */}
      <section>
        <h4 className="text-sm font-medium mb-2 flex items-center gap-2">
          Excluded users
          <Badge variant="destructive" className="text-xs">
            Excluded beats included
          </Badge>
        </h4>
        <textarea
          className="w-full rounded border border-input bg-background px-3 py-2 text-sm font-mono resize-y min-h-16"
          placeholder={'blocked-user-1\nblocked-user-2'}
          value={draft.excluded.join('\n')}
          onChange={(e) =>
            update({
              excluded: e.target.value
                .split('\n')
                .map((v) => v.trim())
                .filter(Boolean),
            })
          }
        />
        <p className="text-xs text-muted-foreground mt-1">
          {draft.excluded.length} users excluded (overrides included list)
        </p>
      </section>

      {/* Save */}
      {dirty && (
        <div className="flex items-center gap-3">
          <Button
            onClick={() => saveMutation.mutate()}
            disabled={saveMutation.isPending}
          >
            <Save className="h-4 w-4 mr-2" />
            {saveMutation.isPending ? 'Saving…' : 'Save segment'}
          </Button>
          <Button
            variant="ghost"
            onClick={() => {
              setDraft({ ...segment })
              setDirty(false)
            }}
          >
            Discard
          </Button>
        </div>
      )}
    </div>
  )
}
