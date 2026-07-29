import { useState, useEffect } from 'react'
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { FlagList } from '@/components/FlagList'
import { FlagDetail } from '@/components/FlagDetail'
import { PropagationMonitor } from '@/components/PropagationMonitor'
import { ExperimentDashboard } from '@/components/ExperimentDashboard'
import { SegmentManager } from '@/components/SegmentManager'
import { AuditLog } from '@/components/AuditLog'
import { LoginPage } from '@/components/LoginPage'
import { authStorage, logout } from '@/lib/auth'
import { api, type Flag } from '@/lib/api'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 5000, retry: 1 },
  },
})

type Tab = 'flags' | 'experiments' | 'segments' | 'propagation' | 'audit'

const TABS: { id: Tab; label: string }[] = [
  { id: 'flags', label: 'Flags' },
  { id: 'experiments', label: 'Experiments' },
  { id: 'segments', label: 'Segments' },
  { id: 'propagation', label: 'Propagation' },
  { id: 'audit', label: 'Audit Log' },
]

// ─── Project/Env switcher selects ───────────────────────────────────────────

function ProjectEnvSwitcher({
  projectKey,
  envKey,
  onProjectChange,
  onEnvChange,
}: {
  projectKey: string
  envKey: string
  onProjectChange: (key: string) => void
  onEnvChange: (key: string) => void
}) {
  const { data: projects = [] } = useQuery({
    queryKey: ['projects'],
    queryFn: () => api.listProjects(),
  })

  const { data: environments = [] } = useQuery({
    queryKey: ['environments', projectKey],
    queryFn: () => api.listEnvironments(projectKey),
    enabled: !!projectKey,
  })

  return (
    <div className="flex items-center gap-2 text-sm">
      <select
        className="rounded border border-input bg-background px-2 py-1 text-sm text-foreground"
        value={projectKey}
        onChange={e => onProjectChange(e.target.value)}
        title="Project"
      >
        {projects.length === 0 && (
          <option value={projectKey}>{projectKey}</option>
        )}
        {projects.map(p => (
          <option key={p.key} value={p.key}>{p.name || p.key}</option>
        ))}
      </select>

      <span className="text-muted-foreground">/</span>

      <select
        className="rounded border border-input bg-background px-2 py-1 text-sm text-foreground"
        value={envKey}
        onChange={e => onEnvChange(e.target.value)}
        title="Environment"
      >
        {environments.length === 0 && (
          <option value={envKey}>{envKey}</option>
        )}
        {environments.map(env => (
          <option key={env.key} value={env.key}>{env.name || env.key}</option>
        ))}
      </select>
    </div>
  )
}

// ─── Inner app (rendered only when authed) ──────────────────────────────────

function AppInner() {
  const [activeTab, setActiveTab] = useState<Tab>('flags')
  const [selectedFlag, setSelectedFlag] = useState<Flag | null>(null)
  const [projectKey, setProjectKey] = useState('default')
  const [envKey, setEnvKey] = useState('production')

  const sdkKey = `sdk-client-${projectKey}-${envKey}`

  // Reset selected flag when project changes
  useEffect(() => {
    setSelectedFlag(null)
  }, [projectKey])

  function handleProjectChange(key: string) {
    setProjectKey(key)
    setEnvKey('production')
  }

  return (
    <div className="min-h-screen bg-background text-foreground">
      {/* Top nav */}
      <header className="border-b border-border px-6 py-3 flex items-center gap-4">
        <h1 className="text-lg font-semibold shrink-0">Pennant</h1>

        <ProjectEnvSwitcher
          projectKey={projectKey}
          envKey={envKey}
          onProjectChange={handleProjectChange}
          onEnvChange={setEnvKey}
        />

        {/* Tab bar */}
        <nav className="flex items-center gap-1 ml-4">
          {TABS.map((tab) => (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id)}
              className={`px-3 py-1.5 rounded-md text-sm font-medium transition-colors ${
                activeTab === tab.id
                  ? 'bg-accent text-foreground'
                  : 'text-muted-foreground hover:text-foreground hover:bg-accent/50'
              }`}
            >
              {tab.label}
            </button>
          ))}
        </nav>

        <div className="ml-auto">
          <button
            onClick={logout}
            className="text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            Sign out
          </button>
        </div>
      </header>

      {/* Flags tab — keeps sidebar + detail layout */}
      {activeTab === 'flags' && (
        <div className="flex h-[calc(100vh-49px)]">
          <aside className="w-72 border-r border-border overflow-y-auto py-4">
            <FlagList
              projectKey={projectKey}
              envKey={envKey}
              onSelectFlag={setSelectedFlag}
            />
          </aside>
          <main className="flex-1 overflow-y-auto">
            {selectedFlag ? (
              <FlagDetail
                flag={selectedFlag}
                projectKey={projectKey}
                envKey={envKey}
              />
            ) : (
              <div className="text-center text-muted-foreground py-12">
                Select a flag from the sidebar
              </div>
            )}
          </main>
        </div>
      )}

      {/* Experiments tab */}
      {activeTab === 'experiments' && (
        <main className="p-6 max-w-5xl mx-auto">
          <ExperimentDashboard projectKey={projectKey} />
        </main>
      )}

      {/* Segments tab */}
      {activeTab === 'segments' && (
        <main className="p-6 h-[calc(100vh-49px)]">
          <SegmentManager projectKey={projectKey} />
        </main>
      )}

      {/* Propagation tab */}
      {activeTab === 'propagation' && (
        <main className="p-6 max-w-2xl mx-auto">
          <h2 className="text-lg font-semibold mb-4">SSE Propagation</h2>
          <PropagationMonitor sdkKey={sdkKey} />
        </main>
      )}

      {/* Audit Log tab */}
      {activeTab === 'audit' && (
        <main className="p-6 max-w-3xl mx-auto">
          <h2 className="text-lg font-semibold mb-4">Audit Log</h2>
          <div className="rounded-lg border border-border">
            <AuditLog projectKey={projectKey} />
          </div>
        </main>
      )}
    </div>
  )
}

// ─── Root with auth gate ─────────────────────────────────────────────────────

function AuthGate() {
  const [authed, setAuthed] = useState(() => !!authStorage.getToken())

  if (!authed) {
    return <LoginPage onLogin={() => setAuthed(true)} />
  }

  return <AppInner />
}

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <AuthGate />
    </QueryClientProvider>
  )
}

export default App
