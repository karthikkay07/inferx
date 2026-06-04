import { useParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
  ScatterChart, Scatter, Legend,
} from 'recharts'
import { jobsApi, type BenchmarkResult, type Job } from '../api/client'

const TERMINAL = new Set(['completed', 'failed', 'cancelled'])

function isTerminal(state: string) {
  return TERMINAL.has(state)
}

// ── best-value highlighting ───────────────────────────────────────────────────

function bestIdx(results: BenchmarkResult[], key: keyof BenchmarkResult, higherBetter: boolean): number {
  if (results.length === 0) return 0
  let best = 0
  for (let i = 1; i < results.length; i++) {
    const v = results[i][key] as number
    const bv = results[best][key] as number
    if (higherBetter ? v > bv : v < bv) best = i
  }
  return best
}

// ── results table ─────────────────────────────────────────────────────────────

function ResultsTable({ results }: { results: BenchmarkResult[] }) {
  if (results.length === 0) {
    return <p className="text-sm text-gray-400 py-4">No results yet.</p>
  }

  const cols: Array<{ label: string; key: keyof BenchmarkResult; higherBetter: boolean; fmt: (v: number) => string }> = [
    { label: 'TTFT P50', key: 'ttft_p50_ms', higherBetter: false, fmt: v => `${v.toFixed(1)} ms` },
    { label: 'TTFT P99', key: 'ttft_p99_ms', higherBetter: false, fmt: v => `${v.toFixed(1)} ms` },
    { label: 'Tok/s',    key: 'tok_per_s',   higherBetter: true,  fmt: v => v.toFixed(0) },
    { label: 'GPU Mem',  key: 'gpu_mem_mb',  higherBetter: false, fmt: v => `${v} MB` },
    { label: 'Cost/MTok',key: 'cost_per_mtok',higherBetter: false, fmt: v => `$${v.toFixed(4)}` },
    { label: 'KV Hit',   key: 'kv_cache_hit',higherBetter: true,  fmt: v => `${(v * 100).toFixed(1)}%` },
  ]

  const bests = cols.map(c => bestIdx(results, c.key, c.higherBetter))

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-gray-500 border-b border-gray-100">
            <th className="pb-2 font-medium pr-4">Engine</th>
            {cols.map(c => <th key={c.key} className="pb-2 font-medium pr-4">{c.label}</th>)}
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-50">
          {results.map((r, ri) => (
            <tr key={r.engine}>
              <td className="py-2 pr-4 font-medium text-gray-700">{r.engine}</td>
              {cols.map((c, ci) => (
                <td
                  key={c.key}
                  className={`py-2 pr-4 ${bests[ci] === ri ? 'bg-green-50 text-green-700 font-semibold rounded' : 'text-gray-600'}`}
                >
                  {c.fmt(r[c.key] as number)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

// ── charts ────────────────────────────────────────────────────────────────────

function CostComparisonChart({ results }: { results: BenchmarkResult[] }) {
  if (results.length === 0) {
    return <div className="flex items-center justify-center h-40 text-gray-400 text-sm">No data yet</div>
  }
  const data = results.map(r => ({ engine: r.engine, cost: r.cost_per_mtok }))
  return (
    <ResponsiveContainer width="100%" height={180}>
      <BarChart data={data}>
        <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" />
        <XAxis dataKey="engine" tick={{ fontSize: 11 }} />
        <YAxis tick={{ fontSize: 11 }} />
        <Tooltip formatter={(v: number) => `$${v.toFixed(4)}`} />
        <Bar dataKey="cost" name="Cost/MTok ($)" fill="#3b82f6" radius={[4, 4, 0, 0]} />
      </BarChart>
    </ResponsiveContainer>
  )
}

function ThroughputVsLatencyChart({ results }: { results: BenchmarkResult[] }) {
  if (results.length === 0) {
    return <div className="flex items-center justify-center h-40 text-gray-400 text-sm">No data yet</div>
  }
  const data = results.map(r => ({ tok_per_s: r.tok_per_s, ttft_p50_ms: r.ttft_p50_ms, engine: r.engine }))
  return (
    <ResponsiveContainer width="100%" height={180}>
      <ScatterChart>
        <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" />
        <XAxis type="number" dataKey="tok_per_s" name="Tok/s" tick={{ fontSize: 11 }} />
        <YAxis type="number" dataKey="ttft_p50_ms" name="TTFT P50 (ms)" tick={{ fontSize: 11 }} />
        <Tooltip cursor={{ strokeDasharray: '3 3' }} />
        <Legend />
        <Scatter data={data} fill="#3b82f6" name="Engine" />
      </ScatterChart>
    </ResponsiveContainer>
  )
}

// ── recommendation card ───────────────────────────────────────────────────────

function RecommendationCard({ results }: { results: BenchmarkResult[] }) {
  if (results.length === 0) return null
  const best = results.reduce((a, b) => (a.tok_per_s > b.tok_per_s ? a : b))
  return (
    <div className="bg-blue-50 border border-blue-200 rounded-xl p-5">
      <h3 className="text-sm font-semibold text-blue-800 mb-2">Recommendation</h3>
      <p className="text-sm text-blue-700">
        Use <strong>{best.engine}</strong> — highest throughput at{' '}
        <strong>{best.tok_per_s.toFixed(0)} tok/s</strong> ($
        {best.cost_per_mtok.toFixed(4)}/MTok)
      </p>
      {Object.keys(best.config).length > 0 && (
        <pre className="mt-3 text-xs bg-white border border-blue-100 rounded p-3 overflow-x-auto text-gray-700">
          {JSON.stringify(best.config, null, 2)}
        </pre>
      )}
    </div>
  )
}

// ── JobDetail page ────────────────────────────────────────────────────────────

export default function JobDetail() {
  const { jobId } = useParams<{ jobId: string }>()

  const { data: job, isLoading: jobLoading, isError: jobError } = useQuery<Job>({
    queryKey: ['job', jobId],
    queryFn: () => jobsApi.get(jobId!).then(r => r.data),
    refetchInterval: (query) => {
      const j = query.state.data
      return j && isTerminal(j.state) ? false : 5_000
    },
    enabled: !!jobId,
  })

  const { data: results } = useQuery<BenchmarkResult[]>({
    queryKey: ['results', jobId],
    queryFn: () => jobsApi.getResults(jobId!).then(r => r.data),
    enabled: !!job && isTerminal(job.state),
    refetchOnWindowFocus: false,
  })

  if (jobLoading) return <div className="text-gray-400 text-sm">Loading...</div>
  if (jobError || !job) return <div className="text-red-500 text-sm">Job not found.</div>

  const safeResults = results ?? []

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold text-gray-900 font-mono">{job.id.slice(0, 8)}</h1>
        <p className="text-sm text-gray-500 mt-1">{job.model}</p>
      </div>

      {/* Metadata */}
      <div className="bg-white rounded-xl border border-gray-200 p-5 grid grid-cols-2 md:grid-cols-3 gap-4 text-sm">
        <div><span className="text-gray-500">State</span><br /><span className="font-medium">{job.state}</span></div>
        <div><span className="text-gray-500">Engines</span><br /><span className="font-medium">{job.engines.join(', ')}</span></div>
        <div><span className="text-gray-500">GPU Profile</span><br /><span className="font-medium">{job.gpu_profile}</span></div>
        <div><span className="text-gray-500">Created</span><br /><span className="font-medium">{new Date(job.created_at).toLocaleString()}</span></div>
        <div><span className="text-gray-500">Updated</span><br /><span className="font-medium">{new Date(job.updated_at).toLocaleString()}</span></div>
        {job.error_msg && <div className="col-span-2 text-red-600"><span className="text-gray-500">Error</span><br />{job.error_msg}</div>}
      </div>

      {/* In-progress state */}
      {!isTerminal(job.state) && (
        <div className="flex items-center gap-3 bg-blue-50 border border-blue-200 rounded-xl p-5">
          <div className="w-5 h-5 border-2 border-blue-500 border-t-transparent rounded-full animate-spin shrink-0" />
          <span className="text-sm text-blue-700">Benchmark in progress...</span>
        </div>
      )}

      {/* Results */}
      {job.state === 'completed' && (
        <>
          <div className="bg-white rounded-xl border border-gray-200 p-5">
            <h2 className="text-sm font-semibold text-gray-700 mb-4">Results</h2>
            <ResultsTable results={safeResults} />
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="bg-white rounded-xl border border-gray-200 p-5">
              <h2 className="text-sm font-semibold text-gray-700 mb-4">Cost per MTok</h2>
              <CostComparisonChart results={safeResults} />
            </div>
            <div className="bg-white rounded-xl border border-gray-200 p-5">
              <h2 className="text-sm font-semibold text-gray-700 mb-4">Throughput vs Latency</h2>
              <ThroughputVsLatencyChart results={safeResults} />
            </div>
          </div>

          <RecommendationCard results={safeResults} />
        </>
      )}
    </div>
  )
}
