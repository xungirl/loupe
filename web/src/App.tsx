import { Route, Routes } from 'react-router-dom'
import Layout from './components/Layout'
import SessionsPage from './pages/SessionsPage'
import TracePage from './pages/TracePage'
import LivePage from './pages/LivePage'
import FindingsPage from './pages/FindingsPage'
import InsightsPage from './pages/InsightsPage'
import ExperimentsPage from './pages/ExperimentsPage'
import JourneyPage from './pages/JourneyPage'
import { EmptyState } from './components/ui'

export default function App() {
  return (
    <Routes>
      {/* Layout renders the sidebar and an <Outlet/> where the page goes. */}
      <Route element={<Layout />}>
        <Route path="/" element={<SessionsPage />} />
        <Route path="/traces/:id" element={<TracePage />} />
        <Route path="/live" element={<LivePage />} />
        <Route path="/findings" element={<FindingsPage />} />
        <Route path="/insights" element={<InsightsPage />} />
        <Route path="/experiments" element={<ExperimentsPage />} />
        <Route path="/journey" element={<JourneyPage />} />
        <Route path="*" element={<EmptyState title="Page not found" body="Pick a page from the sidebar." />} />
      </Route>
    </Routes>
  )
}
