import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import type { Experiment, ExperimentResults, VariantMetric } from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ReferenceLine,
  ResponsiveContainer,
  Cell,
} from 'recharts'
import { useState } from 'react'
import { AlertTriangle, CheckCircle, Clock, TrendingUp } from 'lucide-react'

interface ExperimentDashboardProps {
  projectKey: string
}

export function ExperimentDashboard({ projectKey }: ExperimentDashboardProps) {
  const [selectedExp, setSelectedExp] = useState<string | null>(null)

  const { data: experiments = [] } = useQuery({
    queryKey: ['experiments', projectKey],
    queryFn: () => api.listExperiments(projectKey),
    refetchInterval: 30000,
  })

  const { data: results = null } = useQuery({
    queryKey: ['experimentResults', projectKey, selectedExp],
    queryFn: () =>
      selectedExp ? api.getExperimentResults(projectKey, selectedExp) : null,
    enabled: !!selectedExp,
    refetchInterval: 60000,
  })

  const selectedExperiment = experiments.find((e) => e.key === selectedExp)

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">Experiments</h2>
        <Badge variant="outline">{experiments.length} total</Badge>
      </div>

      {/* Experiment list */}
      <div className="grid gap-3">
        {experiments.length === 0 && (
          <div className="rounded-lg border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
            No experiments yet. Create one by linking a flag to an experiment.
          </div>
        )}
        {experiments.map((exp) => (
          <ExperimentCard
            key={exp.key}
            experiment={exp}
            selected={selectedExp === exp.key}
            onSelect={() =>
              setSelectedExp(exp.key === selectedExp ? null : exp.key)
            }
          />
        ))}
      </div>

      {/* Results panel */}
      {selectedExp && results && selectedExperiment && (
        <ResultsPanel experiment={selectedExperiment} results={results} />
      )}
    </div>
  )
}

function ExperimentCard({
  experiment,
  selected,
  onSelect,
}: {
  experiment: Experiment
  selected: boolean
  onSelect: () => void
}) {
  const statusColors: Record<
    Experiment['status'],
    'secondary' | 'success' | 'outline' | 'destructive' | 'default'
  > = {
    draft: 'secondary',
    running: 'success',
    paused: 'outline',
    stopped: 'secondary',
    archived: 'secondary',
  }

  const StatusIcon =
    {
      running: TrendingUp,
      paused: Clock,
      draft: Clock,
      stopped: CheckCircle,
      archived: CheckCircle,
    }[experiment.status] ?? Clock

  return (
    <div
      onClick={onSelect}
      className={`rounded-lg border p-4 cursor-pointer transition-colors ${
        selected
          ? 'border-primary bg-accent/30'
          : 'border-border hover:bg-accent/20'
      }`}
    >
      <div className="flex items-start justify-between">
        <div>
          <h3 className="font-medium text-sm">{experiment.name}</h3>
          <p className="text-xs text-muted-foreground font-mono mt-0.5">
            {experiment.key}
          </p>
        </div>
        <Badge variant={statusColors[experiment.status]}>
          <StatusIcon className="h-3 w-3 mr-1" />
          {experiment.status}
        </Badge>
      </div>
      <div className="mt-2 flex items-center gap-4 text-xs text-muted-foreground">
        <span>
          Flag: <code className="font-mono">{experiment.flagKey}</code>
        </span>
        <span>{experiment.variations.length} variations</span>
        <span>
          {experiment.metrics.length} metric
          {experiment.metrics.length !== 1 ? 's' : ''}
        </span>
        {experiment.startedAt && (
          <span>
            Started {new Date(experiment.startedAt).toLocaleDateString()}
          </span>
        )}
      </div>
    </div>
  )
}

function ResultsPanel({
  experiment,
  results,
}: {
  experiment: Experiment
  results: ExperimentResults
}) {
  return (
    <div className="rounded-lg border border-border p-6 space-y-6">
      <div className="flex items-center justify-between">
        <h3 className="font-semibold">Results: {experiment.name}</h3>
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <span>{results.totalSamples.toLocaleString()} samples</span>
          <span>·</span>
          <span>
            updated {new Date(results.computedAt).toLocaleTimeString()}
          </span>
        </div>
      </div>

      {/* SRM Warning */}
      {results.srm?.mismatch && (
        <div className="flex items-start gap-3 rounded-lg border border-yellow-200 bg-yellow-50 p-4">
          <AlertTriangle className="h-5 w-5 text-yellow-600 shrink-0 mt-0.5" />
          <div>
            <p className="text-sm font-medium text-yellow-800">
              Sample Ratio Mismatch Detected
            </p>
            <p className="text-xs text-yellow-700 mt-1">
              χ² = {results.srm.chiSquare.toFixed(2)}, p ={' '}
              {results.srm.pValue.toFixed(4)}. The ratio of users in each
              variation does not match the configured weights. Results may be
              invalid — check assignment logic before drawing conclusions.
            </p>
          </div>
        </div>
      )}

      {/* Metric results */}
      {results.metricResults?.map((mr) => (
        <MetricResultPanel
          key={mr.metricKey}
          metricResult={mr}
          alpha={experiment.alpha}
        />
      ))}

      {(!results.metricResults || results.metricResults.length === 0) && (
        <div className="text-center text-sm text-muted-foreground py-8">
          No metric data yet. Track conversion events with{' '}
          <code className="font-mono">POST /sdk/v1/track</code>.
        </div>
      )}
    </div>
  )
}

function MetricResultPanel({
  metricResult,
  alpha,
}: {
  metricResult: { metricKey: string; metricName: string; variants: VariantMetric[] }
  alpha: number
}) {
  const chartData = metricResult.variants.map((v) => ({
    name: v.name || `Variation ${v.variationIndex}`,
    effect: +(v.relativeEffect * 100).toFixed(2),
    ciLower: +(v.ciLower * 100).toFixed(2),
    ciUpper: +(v.ciUpper * 100).toFixed(2),
    significant: v.significant,
    pValue: v.pValue,
  }))

  return (
    <div className="space-y-4">
      <h4 className="text-sm font-medium">{metricResult.metricName}</h4>

      {/* Effect size bar chart */}
      <div className="h-40">
        <ResponsiveContainer width="100%" height="100%">
          <BarChart
            data={chartData}
            layout="vertical"
            margin={{ left: 80, right: 20 }}
          >
            <CartesianGrid strokeDasharray="3 3" horizontal={false} />
            <XAxis
              type="number"
              tickFormatter={(v: number) => `${v}%`}
              domain={['auto', 'auto']}
            />
            <YAxis
              type="category"
              dataKey="name"
              width={75}
              tick={{ fontSize: 12 }}
            />
            <Tooltip
              formatter={(v) => [`${v}%`, 'Relative lift']}
              labelFormatter={(label) => String(label)}
            />
            <ReferenceLine x={0} stroke="#888" strokeDasharray="4 4" />
            <Bar dataKey="effect" name="Relative lift" radius={[0, 4, 4, 0]}>
              {chartData.map((d, i) => (
                <Cell
                  key={i}
                  fill={
                    d.significant
                      ? d.effect > 0
                        ? '#22c55e'
                        : '#ef4444'
                      : '#94a3b8'
                  }
                />
              ))}
            </Bar>
          </BarChart>
        </ResponsiveContainer>
      </div>

      {/* Detailed table */}
      <div className="overflow-x-auto">
        <table className="w-full text-xs">
          <thead>
            <tr className="border-b border-border text-muted-foreground">
              <th className="text-left py-2 pr-4">Variation</th>
              <th className="text-right pr-4">Control rate</th>
              <th className="text-right pr-4">Treatment rate</th>
              <th className="text-right pr-4">Lift</th>
              <th className="text-right pr-4">95% CI</th>
              <th className="text-right pr-4">p-value</th>
              <th className="text-right">Result</th>
            </tr>
          </thead>
          <tbody>
            {metricResult.variants.map((v, i) => (
              <tr key={i} className="border-b border-border/50">
                <td className="py-2 pr-4 font-medium">
                  {v.name || `Var ${v.variationIndex}`}
                </td>
                <td className="text-right pr-4">
                  {(v.controlRate * 100).toFixed(2)}%
                </td>
                <td className="text-right pr-4">
                  {(v.treatmentRate * 100).toFixed(2)}%
                </td>
                <td
                  className={`text-right pr-4 font-mono ${
                    v.absoluteEffect > 0 ? 'text-green-600' : 'text-red-600'
                  }`}
                >
                  {v.absoluteEffect > 0 ? '+' : ''}
                  {(v.absoluteEffect * 100).toFixed(3)}pp
                </td>
                <td className="text-right pr-4 font-mono text-muted-foreground">
                  [{(v.ciLower * 100).toFixed(3)},{' '}
                  {(v.ciUpper * 100).toFixed(3)}]
                </td>
                <td
                  className={`text-right pr-4 font-mono ${
                    v.pValue < alpha ? 'font-bold' : ''
                  }`}
                >
                  {v.pValue < 0.001 ? '<0.001' : v.pValue.toFixed(4)}
                </td>
                <td className="text-right">
                  <Badge
                    variant={
                      v.significant
                        ? v.absoluteEffect > 0
                          ? 'success'
                          : 'destructive'
                        : 'secondary'
                    }
                  >
                    {v.significant
                      ? v.absoluteEffect > 0
                        ? 'Winner ↑'
                        : 'Loser ↓'
                      : 'No sig.'}
                  </Badge>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
