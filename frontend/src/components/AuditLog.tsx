import { useQuery } from '@tanstack/react-query'
import { Badge } from '@/components/ui/badge'

interface AuditLogProps {
  projectKey: string
}

export function AuditLog({ projectKey }: AuditLogProps) {
  const { data: entries = [], isLoading } = useQuery({
    queryKey: ['audit', projectKey],
    queryFn: async () => {
      const resp = await fetch(`/api/v1/projects/${projectKey}/audit?limit=50`)
      if (!resp.ok) return []
      return resp.json()
    },
    refetchInterval: 10000,
  })

  if (isLoading) return <div className="p-4 text-muted-foreground text-sm">Loading…</div>

  return (
    <div className="space-y-2">
      {entries.length === 0 ? (
        <p className="text-sm text-muted-foreground p-4">No audit events yet.</p>
      ) : (
        <ul className="divide-y divide-border">
          {entries.map((e: any, i: number) => (
            <li key={i} className="px-4 py-2 text-xs">
              <div className="flex items-center gap-2">
                <Badge variant="outline">{e.action}</Badge>
                <code className="font-mono text-muted-foreground">{e.resource_id}</code>
                <span className="ml-auto text-muted-foreground">
                  {new Date(e.at).toLocaleString()}
                </span>
              </div>
              <div className="text-muted-foreground mt-0.5">{e.actor}</div>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
