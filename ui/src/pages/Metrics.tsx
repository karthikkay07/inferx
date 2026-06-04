import { useState, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip,
  ResponsiveContainer, Legend,
} from 'recharts'
import { metricsApi, type BenchmarkResult } from '../api/client'

const TIME_RANGES = ['1h', '6h', '24h', '7d'] as const
type TimeRange = typeof TIME_RANGES[number]

function MetricsLineChart({ data, dataKey, name, color }: {
  data: BenchmarkResult[]
  dataKey: keyof BenchmarkResult
  name: string
  color: string
}) {
  if (data.length === 0) {
    return (
      <div className="flex items-center justify-center h-48 text-gray-400 text-sm">
        No data yet
      </div>
    )
  }
  const chartData = data.map((r, i) => ({ index: i + 1, [dataKey]: r[dataKey] }))
  return (
    <ResponsiveContainer width="100%" height={200}>
      <LineChart data={chartData}>
        <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" />
        <XAxis dataKey="index" tick={{ fontSize: 11 }} />
        <YAxis tick={{ fontSize: 11 }} />
        <Tooltip />
        <Legend />
        <Line type="monotone" dataKey={dataKey as string} name={name} stroke={color} dot={false} />
      </LineChart>
    </ResponsiveContainer>
  )
}

function LatencyChart({ data }: { data: BenchmarkResult[] }) {
  if (data.length === 0) {
    return (
      <div className="flex items-center justify-center h-48 text-gray-400 text-sm">
        No data yet
      </div>
    )
  }
  const chartData = data.map((r, i) => ({
    index: i + 1,
    ttft_p50: r.ttft_p50_ms,
    ttft_p99: r.ttft_p99_ms,
  }))
  return (
    <ResponsiveContainer width="100%" height={200}>
      <LineChart data={chartData}>
        <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" />
        <XAxis dataKey="index" tick={{ fontSize: 11 }} />
        <YAxis tick={{ fontSize: 11 }} />
        <Tooltip />
        <Legend />
        <Line type="monotone" dataKey="ttft_p50" name="TTFT P50 (ms)" stroke="#3b82f6" dot={false} />
        <Line type="monotone" dataKey="ttft_p99" name="TTFT P99 (ms)" stroke="#ef4444" dot={false} />
      </LineChart>
    </ResponsiveContainer>
  )
}

function pct(arr: number[], p: number): number {
  if (arr.length === 0) return 0
  const sorted = [...arr].sort((a, b) => a - b)
  const idx = Math.ceil((p / 100) * sorted.length) - 1
  return sorted[Math.max(0, idx)]
}

function SummaryTable({ data }: { data: BenchmarkResult[] }) {
  if (data.length === 0) return null

  const toks = data.map(r => r.tok_per_s)
  const ttfts = data.map(r => r.ttft_p50_ms)
  const costs = data.map(r => r.cost_per_mtok)

  const avg = (arr: number[]) => arr.reduce((a, b) => a + b, 0) / arr.length

  const rows = [
    { metric: 'Tok/s',     min: Math.min(...toks).toFixed(0), max: Math.max(...toks).toFixed(0), mean: avg(toks).toFixed(0), p95: pct(toks, 95).toFixed(0) },
    { metric: 'TTFT P50 (ms)', min: Math.min(...ttfts).toFixed(1), max: Math.max(...ttfts).toFixed(1), mean: avg(ttfts).toFixed(1), p95: pct(ttfts, 95).toFixed(1) },
    { metric: 'Cost/MTok ($)', min: Math.min(...costs).toFixed(4), max: Math.max(...costs).toFixed(4), mean: avg(costs).toFixed(4), p95: pct(costs, 95).toFixed(4) },
  ]

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-gray-500 border-b border-gray-100">
            <th className="pb-2 font-medium">Metric</th>
            <th className="pb-2 font-medium">Min</th>
            <th className="pb-2 font-medium">Max</th>
            <th className="pb-2 font-medium">Mean</th>
            <th className="pb-2 font-medium">P95</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-50">
          {rows.map(r => (
            <tr key={r.metric}>
              <td className="py-2 text-gray-700 font-medium">{r.metric}</td>
              <td className="py-2 text-gray-600">{r.min}</td>
              <td className="py-2 text-gray-600">{r.max}</td>
              <td className="py-2 text-gray-600">{r.mean}</td>
              <td className="py-2 text-gray-600">{r.p95}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export default function Metrics() {
  const [engine, setEngine] = useState('')
  const [model, setModel] = useState('')
  const [timeRange, setTimeRange] = useState<TimeRange>('24h')

  const enabled = engine.trim() !== '' && model.trim() !== ''

  const { data, isLoading, isError } = useQuery({
    queryKey: ['metrics', engine, model, timeRange],
    queryFn: () => metricsApi.query(engine, model, timeRange).then(r => r.data),
    enabled,
  })

  const results = useMemo(() => data ?? [], [data])

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold text-gray-900">Metrics Explorer</h1>
        <p className="text-sm text-gray-500 mt-1">Query historical benchmark results</p>
      </div>

      {/* Filters */}
      <div className="bg-white rounded-xl border border-gray-200 p-5 flex flex-wrap gap-4 items-end">
        <div>
          <label className="text-xs text-gray-500 block mb-1">Engine</label>
          <input
            className="border border-gray-200 rounded-lg px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
            placeholder="vllm"
            value={engine}
            onChange={e => setEngine(e.target.value)}
          />
        </div>
        <div>
          <label className="text-xs text-gray-500 block mb-1">Model</label>
          <input
            className="border border-gray-200 rounded-lg px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
            placeholder="meta-llama/Llama-3.1-8B"
            value={model}
            onChange={e => setModel(e.target.value)}
          />
        </div>
        <div>
          <label className="text-xs text-gray-500 block mb-1">Time range</label>
          <div className="flex gap-1">
            {TIME_RANGES.map(r => (
              <button
                key={r}
                onClick={() => setTimeRange(r)}
                className={`px-3 py-1.5 text-xs rounded-lg font-medium transition-colors ${
                  timeRange === r
                    ? 'bg-blue-600 text-white'
                    : 'bg-gray-100 text-gray-600 hover:bg-gray-200'
                }`}
              >
                {r}
              </button>
            ))}
          </div>
        </div>
      </div>

      {!enabled && (
        <p className="text-sm text-gray-400">Enter engine and model to query metrics.</p>
      )}

      {isLoading && <p className="text-sm text-gray-400">Loading...</p>}
      {isError && <p className="text-sm text-red-500">Failed to load metrics.</p>}

      {enabled && !isLoading && (
        <>
          <div className="bg-white rounded-xl border border-gray-200 p-5">
            <h2 className="text-sm font-semibold text-gray-700 mb-4">Throughput (tok/s)</h2>
            <MetricsLineChart data={results} dataKey="tok_per_s" name="Tok/s" color="#3b82f6" />
          </div>

          <div className="bg-white rounded-xl border border-gray-200 p-5">
            <h2 className="text-sm font-semibold text-gray-700 mb-4">Latency</h2>
            <LatencyChart data={results} />
          </div>

          <div className="bg-white rounded-xl border border-gray-200 p-5">
            <h2 className="text-sm font-semibold text-gray-700 mb-4">Summary Statistics</h2>
            {results.length === 0
              ? <p className="text-sm text-gray-400">No data for this filter.</p>
              : <SummaryTable data={results} />
            }
          </div>
        </>
      )}
    </div>
  )
}
