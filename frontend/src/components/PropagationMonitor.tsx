import { useEffect, useRef, useState } from 'react'
import { Badge } from '@/components/ui/badge'

interface StreamEvent {
  id: number
  event: string
  version?: number
  receivedAt: Date
  checksum?: string
}

interface PropagationMonitorProps {
  sdkKey: string
}

export function PropagationMonitor({ sdkKey }: PropagationMonitorProps) {
  const [events, setEvents] = useState<StreamEvent[]>([])
  const [connected, setConnected] = useState(false)
  const esRef = useRef<EventSource | null>(null)
  const idRef = useRef(0)

  useEffect(() => {
    const url = `/sdk/v1/stream?sdkKey=${encodeURIComponent(sdkKey)}`
    const es = new EventSource(url)
    esRef.current = es

    es.onopen = () => setConnected(true)
    es.onerror = () => setConnected(false)

    const handleEvent = (type: string) => (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data) as { version?: number; checksum?: string }
        setEvents(prev => [
          {
            id: ++idRef.current,
            event: type,
            version: data.version,
            checksum: data.checksum,
            receivedAt: new Date(),
          },
          ...prev.slice(0, 49), // keep last 50
        ])
      } catch {
        // ignore
      }
    }

    es.addEventListener('put', handleEvent('put'))
    es.addEventListener('patch', handleEvent('patch'))

    return () => {
      es.close()
      setConnected(false)
    }
  }, [sdkKey])

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <div className={`h-2 w-2 rounded-full ${connected ? 'bg-green-500' : 'bg-red-500'}`} />
        <span className="text-sm font-medium">{connected ? 'Connected' : 'Disconnected'}</span>
        <Badge variant="outline" className="ml-auto">{events.length} events</Badge>
      </div>

      <div className="rounded-lg border border-border bg-muted/30 overflow-hidden">
        {events.length === 0 ? (
          <div className="p-4 text-center text-sm text-muted-foreground">
            Waiting for SSE events…
          </div>
        ) : (
          <ul className="divide-y divide-border max-h-80 overflow-y-auto">
            {events.map(ev => (
              <li key={ev.id} className="px-3 py-2 text-xs font-mono">
                <div className="flex items-center gap-2">
                  <Badge variant={ev.event === 'put' ? 'default' : 'secondary'} className="text-xs">
                    {ev.event}
                  </Badge>
                  {ev.version && <span className="text-muted-foreground">v{ev.version}</span>}
                  <span className="ml-auto text-muted-foreground">
                    {ev.receivedAt.toLocaleTimeString()}
                  </span>
                </div>
                {ev.checksum && (
                  <div className="text-muted-foreground mt-0.5 truncate">
                    {ev.checksum.slice(0, 20)}…
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
