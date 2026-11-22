import { useEffect, useState, type ReactNode } from 'react'
import { NavLink, Outlet } from 'react-router-dom'
import { api } from '../api'
import Logo from './Logo'

// Tiny inline icons (16px, stroke = currentColor) so no icon library is needed.
const icon = (d: ReactNode) => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    {d}
  </svg>
)
const NAV = [
  { to: '/', label: 'Sessions', icon: icon(<><path d="M3 6h18M3 12h18M3 18h12" /></>) },
  { to: '/live', label: 'Live', icon: icon(<><circle cx="12" cy="12" r="3" /><path d="M5.6 5.6a9 9 0 0 0 0 12.8M18.4 5.6a9 9 0 0 1 0 12.8" /></>) },
  { to: '/findings', label: 'Findings', icon: icon(<><path d="M12 3 2 20h20L12 3z" /><path d="M12 10v4M12 17h.01" /></>) },
  { to: '/insights', label: 'Insights', icon: icon(<><path d="M4 20V10M10 20V4M16 20v-7M22 20H2" /></>) },
  { to: '/experiments', label: 'Experiments', icon: icon(<><path d="M9 3h6M10 3v6L4 20h16L14 9V3" /></>) },
  { to: '/journey', label: 'Journey', icon: icon(<><circle cx="6" cy="5" r="2" /><circle cx="6" cy="19" r="2" /><path d="M6 7v10M10 5h10M10 19h10M10 12h7" /></>) },
]

export default function Layout() {
  return (
    <div className="app">
      <aside className="sidebar">
        <NavLink to="/" className="brand">
          <Logo />
          <div>
            <div className="brand-name">Loupe</div>
            <div className="brand-tag">flight recorder for AI agents</div>
          </div>
        </NavLink>
        <nav className="nav">
          {NAV.map((n) => (
            // `end` makes "/" active only on the exact Sessions page.
            <NavLink key={n.to} to={n.to} end={n.to === '/'} className="nav-link">
              {n.icon}
              <span>{n.label}</span>
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-foot">
          <HealthDot />
          <ThemeToggle />
        </div>
      </aside>
      <main className="main">
        <Outlet />
      </main>
    </div>
  )
}

/** Polls GET /healthz every 10 s and shows a green/red dot. */
function HealthDot() {
  const [state, setState] = useState<{ ok: boolean; queue?: number } | null>(null)
  useEffect(() => {
    const check = () =>
      api
        .health()
        .then((h) => setState({ ok: h.ok, queue: h.queue_depth }))
        .catch(() => setState({ ok: false }))
    check()
    const timer = setInterval(check, 10_000)
    return () => clearInterval(timer)
  }, [])
  const label = state === null ? 'checking…' : state.ok ? `server ok · queue ${state.queue ?? 0}` : 'server unreachable'
  return (
    <div className="health" data-testid="health" title={`${label} (GET /healthz, every 10 s)`}>
      <span className={`dot ${state === null ? 'dot-gray' : state.ok ? 'dot-green' : 'dot-red'}`} />
      <span className="health-label">{label}</span>
    </div>
  )
}

/** Light by default; the choice is remembered in localStorage. */
function ThemeToggle() {
  const [dark, setDark] = useState(() => {
    try {
      return localStorage.getItem('loupe-theme') === 'dark'
    } catch {
      return false
    }
  })
  useEffect(() => {
    document.documentElement.dataset.theme = dark ? 'dark' : 'light'
    try {
      localStorage.setItem('loupe-theme', dark ? 'dark' : 'light')
    } catch {
      /* storage blocked: fine */
    }
  }, [dark])
  return (
    <button className="btn btn-ghost btn-sm theme-toggle" onClick={() => setDark((d) => !d)}>
      {dark ? '☀ Light' : '☾ Dark'}
    </button>
  )
}
