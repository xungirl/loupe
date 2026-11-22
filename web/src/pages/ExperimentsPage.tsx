import { useState } from 'react'
import { api } from '../api'
import { useApi } from '../useApi'
import { EmptyState, ErrorBox, Loading, PageHeader } from '../components/ui'
import ExperimentReportView from './ExperimentReportView'

/** Reports from the preregistered A/B/C experiment (results/experiment/<run>/report.json). */
export default function ExperimentsPage() {
  const res = useApi(() => api.experiments(), [])
  const [index, setIndex] = useState(0)
  const reports = res.data?.reports ?? []
  const report = reports[Math.min(index, reports.length - 1)]

  return (
    <div className="page">
      <PageHeader
        title="Experiments"
        subtitle="Does a planner/coder/reviewer team beat a single agent? Preregistered A/B/C runs, with confidence intervals."
        actions={
          reports.length > 1 && (
            <select className="input" value={index} onChange={(e) => setIndex(Number(e.target.value))} aria-label="Report">
              {reports.map((r, i) => (
                <option key={r.run_id ?? i} value={i}>
                  {r.run_id ?? `report ${i + 1}`}
                  {r.provider === 'fake' || r.is_dry_run ? ' (dry run)' : ''}
                </option>
              ))}
            </select>
          )
        }
      />

      {res.error && <ErrorBox error={res.error} />}
      {res.loading && !res.data && <Loading />}
      {res.data && reports.length === 0 && (
        <EmptyState title="No experiment reports yet" body={<>Nothing in <code>{res.data.dir || 'results/experiment'}</code>.</>}>
          <div className="empty-steps" data-testid="experiments-empty">
            <div>
              <code>make experiment-dryrun</code> — runs the whole pipeline against a fake deterministic provider (free,
              no API key). Its report is clearly marked as a dry run.
            </div>
            <div>
              <code>make experiment</code> — the real run against DashScope; needs <code>DASHSCOPE_API_KEY</code> and
              stops at the spend cap.
            </div>
          </div>
        </EmptyState>
      )}
      {report && <ExperimentReportView report={report} />}
    </div>
  )
}
