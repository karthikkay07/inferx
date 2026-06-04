import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import {
  LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, Legend,
} from 'recharts'
import { jobsApi, type Job } from '../api/client'

// ── stat card ─────────────────────────────────────────────────────────────────

interface StatCardProps {
  label: string
  value: string | number
  sub?: string
}

function StatCard({ label, value, sub }: StatCardProps) {
  return (
    <div className="bg-white rounded-xl border border-gray-200 p-5">
      <p className="text-sm text-gray-500">{label}</p>
      <p className="text-2xl font-bold text-gray-900 mt-1">{value}</p>
      {sub && <p className="text-xs text-gray-400 mt-1">{sub}</p>}
    </div>
  )
}

// ── recent jobs table ─────────────────────────────────────────────────────────

const STATE_COLORS: Record<string, string> = {
  completed:  'bg-green-100 text-green-700',
  running:    'bg-blue-100 text-blue-700',
  pending:    'bg-yellow-100 text-yellow-700',
  failed:     'bg-red-100 text-red-700',
  cancelled:  'bg-gray-100 text-gray-600',
  collecting: 'bg-purple-100 text-purple-700',
  analyzing:  'bg-indigo-100 text-indigo-700',
}

function RecentJobsTable({ jobs }: { jobs: Job[] }) {
  if (jobs.length === 0) {
    return (
      <p className="text-sm text-gray-400 text-center py-8">
        No jobs yet. Run your first benchmark with the CLI.
      </p>
    )
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-left text-gray-500 border-b border-gray-100">
            <th className="pb-2 font-medium">Job ID</th>
            <th className="pb-2 font-medium">Model</th>
            <th className="pb-2 font-medium">Engines</th>
            <th className="pb-2 font-medium">State</th>
            <th className="pb-2 font-medium">Created</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-50">
          {jobs.slice(0, 10).map(job => (
            <tr key={job.id} className="hover:bg-gray-50">
              <td className="py-2">
                <Link to={`/jobs/${job.id}`} className="font-mono text-blue-600 hover:underline">
                  {job.id.slice(0, 8)}
                </Link>
              </td>
              <td className="py-2 text-gray-700">{job.model}</td>
              <td className="py-2 text-gray-500">{job.engines.join(', ')}</td>
              <td className="py-2">
                <span className={`px-2 py-0.5 rounded-full text-xs font-medium ${STATE_COLORS[job.state] ?? 'bg-gray-100 text-gray-600'}`}>
                  {job.state}
                </span>
              </td>
              <td className="py-2 text-gray-400">
                {new Date(job.created_at).toLocaleString()}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

// ── throughput chart ──────────────────────────────────────────────────────────

interface ChartPoint {
  time: string
  tok_per_s: number
  engine: string
}

function ThroughputChart({ data }: { data: ChartPoint[] }) {
  if (data.length === 0) {
    return (
      <div className="flex items-center justify-center h-48 text-gray-400 text-sm">
        No data yet
      </div>
    )
  }
  return (
    <ResponsiveContainer width="100%" height={200}>
      <LineChart data={data}>
        <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" />
        <XAxis dataKey="time" tick={{ fontSize: 11 }} />
        <YAxis tick={{ fontSize: 11 }} />
        <Tooltip />
        <Legend />
        <Line type="monotone" dataKey="tok_per_s" name="Tok/s" stroke="#3b82f6" dot={false} />
      </LineChart>
    </ResponsiveContainer>
  )
}

// ── Dashboard page ────────────────────────────────────────────────────────────

export default function Dashboard() {
  const { data, isLoading, isError } = useQuery({
    queryKey: ['jobs', 'dashboard'],
    queryFn: () => jobsApi.list(undefined, 100).then(r => r.data),
    refetchInterval: 10_000,
  })

  if (isLoading) {
    return <div className="text-gray-400 text-sm">Loading...</div>
  }
  if (isError) {
    return <div className="text-red-500 text-sm">Failed to load jobs.</div>
  }

  const jobs = data?.jobs ?? []
  const today = new Date().toDateString()

  const totalJobs = jobs.length
  const runningJobs = jobs.filter(j => j.state === 'running' || j.state === 'collecting' || j.state === 'analyzing').length
  const completedToday = jobs.filter(
    j => j.state === 'completed' && new Date(j.created_at).toDateString() === today
  ).length

  // Build a simple time-series from job created_at for completed jobs
  const chartData: ChartPoint[] = jobs
    .filter(j => j.state === 'completed')
    .sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime())
    .slice(-20)
    .map(j => ({
      time: new Date(j.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
      tok_per_s: 0,  // would need results fetch — shown as placeholder
      engine: j.engines[0] ?? '',
    }))

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold text-gray-900">Dashboard</h1>
        <p className="text-sm text-gray-500 mt-1">InferBolt benchmark activity</p>
      </div>

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard label="Total Jobs" value={totalJobs} />
        <StatCard label="Running" value={runningJobs} />
        <StatCard label="Completed Today" value={completedToday} />
        <StatCard label="Avg Tok/s" value="—" sub="select a job for details" />
      </div>

      <div className="bg-white rounded-xl border border-gray-200 p-5">
        <h2 className="text-sm font-semibold text-gray-700 mb-4">Recent Jobs</h2>
        <RecentJobsTable jobs={jobs} />
      </div>

      <div className="bg-white rounded-xl border border-gray-200 p-5">
        <h2 className="text-sm font-semibold text-gray-700 mb-4">Throughput (last 24h)</h2>
        <ThroughputChart data={chartData} />
      </div>
    </div>
  )
}
