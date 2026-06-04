import { NavLink } from 'react-router-dom'
import { LayoutDashboard, Briefcase, BarChart2, Cpu } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { healthApi } from '../api/client'
import clsx from 'clsx'

const links = [
  { to: '/', label: 'Dashboard', icon: LayoutDashboard },
  { to: '/jobs', label: 'Jobs', icon: Briefcase },
  { to: '/metrics', label: 'Metrics', icon: BarChart2 },
  { to: '/engines', label: 'Engines', icon: Cpu },
]

export default function Sidebar() {
  const { data } = useQuery({
    queryKey: ['health'],
    queryFn: () => healthApi.check().then(r => r.data),
    refetchInterval: 30_000,
    retry: false,
  })

  const connected = data?.status === 'ok' && data?.postgres === 'ok'

  return (
    <nav className="w-56 bg-white border-r border-gray-200 flex flex-col py-6 px-4 shrink-0">
      <div className="mb-8">
        <span className="font-mono text-xl font-bold text-gray-900 tracking-tight">
          inferbolt
        </span>
      </div>

      <ul className="flex flex-col gap-1 flex-1">
        {links.map(({ to, label, icon: Icon }) => (
          <li key={to}>
            <NavLink
              to={to}
              end={to === '/'}
              className={({ isActive }) =>
                clsx(
                  'flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium transition-colors',
                  isActive
                    ? 'bg-gray-100 text-gray-900'
                    : 'text-gray-600 hover:bg-gray-50 hover:text-gray-900'
                )
              }
            >
              <Icon className="w-4 h-4 shrink-0" />
              {label}
            </NavLink>
          </li>
        ))}
      </ul>

      <div className="flex items-center gap-2 text-xs text-gray-500 mt-4 px-3">
        <span
          className={clsx(
            'w-2 h-2 rounded-full',
            connected ? 'bg-green-500' : 'bg-red-400'
          )}
        />
        {connected ? 'Connected' : 'Disconnected'}
      </div>
    </nav>
  )
}
