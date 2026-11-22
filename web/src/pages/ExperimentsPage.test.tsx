import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import fixture from '../fixtures/experiment-dryrun.json'
import type { ExperimentReport } from '../api'
import ExperimentReportView from './ExperimentReportView'
import ExperimentsPage from './ExperimentsPage'

// JSON imports widen [lo, hi] tuples to number[], hence the double cast.
const report = fixture as unknown as ExperimentReport

// Replace fetch with a function that returns `body` as JSON.
function mockFetch(body: unknown) {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(body), { status: 200 })))
}

afterEach(() => vi.unstubAllGlobals())

describe('ExperimentReportView', () => {
  it('shows the DRY RUN banner for the fake provider', () => {
    render(<ExperimentReportView report={report} />)
    expect(screen.getByTestId('dry-run-banner')).toHaveTextContent(
      'DRY RUN — fake deterministic provider. These numbers exercise the pipeline and are NOT experimental results.',
    )
  })

  it('renders arms, paired CIs, the per-task matrix and the planning label', () => {
    render(<ExperimentReportView report={report} />)
    expect(screen.getByText('planner/coder/reviewer')).toBeInTheDocument()
    expect(screen.getByTestId('arm-table').querySelectorAll('tbody tr')).toHaveLength(3)
    expect(screen.getByTestId('paired-table')).toHaveTextContent('B-A')
    expect(screen.getByTestId('paired-table')).toHaveTextContent('+25 pp')
    const matrix = screen.getByTestId('task-matrix')
    expect(matrix.querySelectorAll('tbody tr')).toHaveLength(4)
    expect(matrix.querySelectorAll('.pf-pass')).toHaveLength(7)
    expect(matrix.querySelectorAll('.pf-fail')).toHaveLength(4)
    expect(screen.getByText(/Planning figure from dry run, not a measurement/)).toBeInTheDocument()
    expect(screen.getByTestId('spend')).toHaveTextContent('¥0.00')
  })

  it('hides the banner for a real provider and tolerates missing fields', () => {
    render(<ExperimentReportView report={{ run_id: 'real-1', provider: 'dashscope', is_dry_run: false }} />)
    expect(screen.queryByTestId('dry-run-banner')).not.toBeInTheDocument()
    expect(screen.getByText('real-1')).toBeInTheDocument()
  })

  it('treats is_dry_run as a dry run even with a real provider name', () => {
    render(<ExperimentReportView report={{ provider: 'dashscope', is_dry_run: true }} />)
    expect(screen.getByTestId('dry-run-banner')).toBeInTheDocument()
  })
})

describe('ExperimentsPage', () => {
  it('loads reports from the API and shows the banner', async () => {
    mockFetch({ reports: [report], dir: 'results/experiment' })
    render(
      <MemoryRouter>
        <ExperimentsPage />
      </MemoryRouter>,
    )
    expect(await screen.findByTestId('dry-run-banner')).toBeInTheDocument()
  })

  it('explains how to produce a report when there are none', async () => {
    mockFetch({ reports: [], dir: 'results/experiment' })
    render(
      <MemoryRouter>
        <ExperimentsPage />
      </MemoryRouter>,
    )
    const empty = await screen.findByTestId('experiments-empty')
    expect(empty).toHaveTextContent('make experiment-dryrun')
    expect(empty).toHaveTextContent('DASHSCOPE_API_KEY')
  })
})
