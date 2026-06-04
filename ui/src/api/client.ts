import axios from 'axios'

const api = axios.create({
  baseURL: import.meta.env.VITE_API_URL ?? 'http://localhost:8080',
})

api.interceptors.request.use(config => {
  const key = localStorage.getItem('inferbolt_api_key')
  if (key) config.headers.Authorization = `Bearer ${key}`
  return config
})

export interface Job {
  id: string
  tenant_id: string
  model: string
  engines: string[]
  state: string
  gpu_profile: string
  created_at: string
  updated_at: string
  error_msg?: string
}

export interface BenchmarkResult {
  job_id: string
  engine: string
  model: string
  ttft_p50_ms: number
  ttft_p99_ms: number
  itl_ms: number
  tok_per_s: number
  gpu_mem_mb: number
  kv_cache_hit: number
  error_rate: number
  cost_per_mtok: number
  config: Record<string, unknown>
}

export interface Recommendation {
  job_id: string
  best_engine: string
  best_config: Record<string, unknown>
  cost_per_mtok: number
  tok_per_sec: number
  reasoning: string
}

export const jobsApi = {
  list: (state?: string, limit = 20) =>
    api.get<{ jobs: Job[]; total: number }>('/v1/jobs', { params: { state, limit } }),
  get: (id: string) =>
    api.get<Job>(`/v1/jobs/${id}`),
  getResults: (id: string) =>
    api.get<BenchmarkResult[]>(`/v1/jobs/${id}/results`),
  create: (body: object) =>
    api.post<{ job_id: string; state: string }>('/v1/jobs', body),
  cancel: (id: string) =>
    api.delete(`/v1/jobs/${id}`),
}

export const metricsApi = {
  query: (engine: string, model: string, since: string) =>
    api.get<BenchmarkResult[]>('/v1/metrics', { params: { engine, model, since } }),
}

export const healthApi = {
  check: () =>
    api.get<{ status: string; postgres: string; version: string }>('/health'),
}
