import type { ReactNode } from 'react'
import type { ExperimentReport, PairedComparison, TaskArmResult } from '../api'
import { formatCNY, formatCompact, formatNumber, formatPercent } from '../format'

/**
 * Renders one experiment report. Reports come from the Python harness and
 * any field may be missing, so every access below has a fallback.
 */
export default function ExperimentReportView({ report: r }: { report: ExperimentReport }) {
  const isFake = r.provider === 'fake' || r.is_dry_run === true
  const arms = r.arms ?? (r.summary_by_arm ?? []).map((a) => a.arm ?? '?')
  const label = (arm: string) => r.arm_labels?.[arm] ?? r.summary_by_arm?.find((a) => a.arm === arm)?.label ?? ''

  return (
    <div className="report" data-testid="experiment-report">
      {isFake && (
        <div className="dry-run-banner" role="alert" data-testid="dry-run-banner">
          <strong>DRY RUN — fake deterministic provider.</strong> These numbers exercise the pipeline and are NOT
          experimental results.
        </div>
      )}

      <div className="card card-pad">
        <div className="report-meta">
          <Meta label="Run" value={<code>{r.run_id ?? '(no id)'}</code>} />
          <Meta label="Provider" value={r.provider ?? '–'} />
          <Meta label="Model" value={r.model ? <code>{r.model}</code> : '–'} />
          <Meta label="Created" value={r.created_at ? new Date(r.created_at).toLocaleString() : '–'} />
          <Meta label="Tasks" value={r.tasks ?? '–'} />
          <Meta label="Arms" value={arms.join(' · ') || '–'} />
          <Meta
            label="Runs"
            value={
              r.runs_planned ? (
                <>
                  {r.runs_completed ?? 0} / {r.runs_planned}
                  <Progress ratio={(r.runs_completed ?? 0) / r.runs_planned} />
                </>
              ) : (
                (r.runs_completed ?? '–')
              )
            }
          />
        </div>
        {r.spend && <SpendBar spend={r.spend} />}
      </div>

      {r.summary_by_arm && r.summary_by_arm.length > 0 && (
        <section className="card card-pad">
          <h2>Results by arm</h2>
          <table className="table table-compact" data-testid="arm-table">
            <thead>
              <tr>
                <th>Arm</th>
                <th className="num">n</th>
                <th>Pass rate (95% CI)</th>
                <th className="num">Mean tokens</th>
                <th className="num">Mean cost</th>
                <th className="num">Steps</th>
                <th className="num">Rework</th>
                <th className="num">Wall time</th>
                <th className="num">Loop findings</th>
              </tr>
            </thead>
            <tbody>
              {r.summary_by_arm.map((a, i) => (
                <tr key={a.arm ?? i}>
                  <td>
                    <span className="arm-tag">{a.arm ?? '?'}</span> <span className="muted">{label(a.arm ?? '')}</span>
                  </td>
                  <td className="num">{a.n ?? '–'}</td>
                  <td>
                    <div className="ci-cell">
                      <span className="ci-text">
                        {formatPercent(a.pass_rate, 0)}
                        {a.pass_ci95 && (
                          <span className="muted small">
                            {' '}
                            [{formatPercent(a.pass_ci95[0], 0)}, {formatPercent(a.pass_ci95[1], 0)}]
                          </span>
                        )}
                      </span>
                      <RateCI rate={a.pass_rate} ci={a.pass_ci95} />
                    </div>
                  </td>
                  <td className="num">{formatCompact(a.mean_tokens)}</td>
                  <td className="num">{formatCNY(a.mean_cost_cny, 4)}</td>
                  <td className="num">{a.mean_steps?.toFixed(1) ?? '–'}</td>
                  <td className="num">{a.mean_rework?.toFixed(2) ?? '–'}</td>
                  <td className="num">{a.mean_wall_s !== undefined ? `${a.mean_wall_s.toFixed(1)} s` : '–'}</td>
                  <td className="num">{a.loop_findings ?? '–'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      )}

      {r.paired && r.paired.length > 0 && (
        <section className="card card-pad">
          <h2>Paired comparisons</h2>
          <p className="muted small">
            Difference between arms on the same tasks. The bar is the 95% confidence interval; if it crosses 0 the
            difference is not significant.
          </p>
          <PairedChart rows={r.paired} />
        </section>
      )}

      {r.per_task && r.per_task.length > 0 && (
        <section className="card card-pad">
          <h2>Per task</h2>
          <div className="task-matrix-wrap">
            <table className="table table-compact task-matrix" data-testid="task-matrix">
              <thead>
                <tr>
                  <th>Task</th>
                  {arms.map((a) => (
                    <th key={a} className="center" title={label(a)}>
                      {a}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {r.per_task.map((row, i) => (
                  <tr key={row.task ?? i}>
                    <td>
                      <code>{row.task ?? `#${i + 1}`}</code>
                    </td>
                    {arms.map((a) => {
                      const cell = row[a] as TaskArmResult | undefined
                      if (!cell || typeof cell !== 'object') return <td key={a} className="center muted">–</td>
                      const tip = `tokens ${formatNumber(cell.tokens)} · steps ${cell.steps ?? '–'} · rework ${cell.rework ?? '–'}`
                      return (
                        <td key={a} className="center" title={tip}>
                          <span className={`pf ${cell.passed ? 'pf-pass' : 'pf-fail'}`}>{cell.passed ? '✓' : '✗'}</span>
                        </td>
                      )
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}

      {r.planning_estimate && (
        <section className="card card-pad planning">
          <h2>Planning estimate</h2>
          <div className="notice notice-amber">
            Planning figure from dry run, not a measurement.
            {r.planning_estimate.label && <span className="muted"> ({r.planning_estimate.label})</span>}
          </div>
          <div className="report-meta">
            {Object.entries(r.planning_estimate.tokens_per_run_by_arm ?? {}).map(([arm, tok]) => (
              <Meta
                key={arm}
                label={`Arm ${arm} tokens / run`}
                value={
                  typeof tok === 'number'
                    ? formatCompact(tok)
                    : `${formatCompact(tok?.input)} in · ${formatCompact(tok?.output)} out`
                }
              />
            ))}
            {r.planning_estimate.projected_cost_cny !== undefined && (
              <Meta label="Projected cost" value={formatCNY(r.planning_estimate.projected_cost_cny)} />
            )}
          </div>
        </section>
      )}

      {r.notes && r.notes.length > 0 && (
        <section className="card card-pad">
          <h2>Notes</h2>
          <ul className="notes">
            {r.notes.map((n, i) => (
              <li key={i}>{n}</li>
            ))}
          </ul>
        </section>
      )}
    </div>
  )
}

function Meta({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="meta">
      <div className="meta-label">{label}</div>
      <div className="meta-value">{value}</div>
    </div>
  )
}

function Progress({ ratio }: { ratio: number }) {
  return (
    <span className="progress">
      <span className="progress-fill" style={{ width: `${Math.min(1, Math.max(0, ratio)) * 100}%` }} />
    </span>
  )
}

function SpendBar({ spend }: { spend: NonNullable<ExperimentReport['spend']> }) {
  const cur = spend.currency === 'CNY' || !spend.currency ? '¥' : `${spend.currency} `
  const spent = spend.spent ?? 0
  const ratio = spend.cap ? spent / spend.cap : 0
  return (
    <div className="spend" data-testid="spend">
      <div className="spend-head">
        <span className="meta-label">Spend vs cap</span>
        <span>
          <strong>
            {cur}
            {spent.toFixed(2)}
          </strong>
          {spend.cap !== undefined && (
            <span className="muted">
              {' '}
              of {cur}
              {spend.cap.toFixed(2)} ({formatPercent(ratio, 0)})
            </span>
          )}
        </span>
      </div>
      <span className="spend-track">
        <span className={`spend-fill ${ratio > 0.9 ? 'is-hot' : ''}`} style={{ width: `${Math.min(1, ratio) * 100}%` }} />
      </span>
    </div>
  )
}

/** A 0–100% track with the CI as a band and the point estimate as a dot. */
function RateCI({ rate, ci }: { rate?: number; ci?: [number, number] }) {
  if (rate === undefined) return null
  const W = 140
  const x = (v: number) => Math.max(0, Math.min(1, v)) * W
  return (
    <svg width={W} height={14} className="ci-svg" aria-hidden="true">
      <line x1={0} x2={W} y1={7} y2={7} className="ci-track" />
      {ci && <rect x={x(ci[0])} y={3} width={Math.max(2, x(ci[1]) - x(ci[0]))} height={8} rx={4} className="ci-band" />}
      <circle cx={x(rate)} cy={7} r={3.5} className="ci-dot" />
    </svg>
  )
}

/**
 * Forest-plot style rows: a shared horizontal axis centred on 0, a line for
 * each 95% CI and a dot for the difference. The axis is symmetric around 0 and
 * wide enough for the largest |CI bound|.
 */
function PairedChart({ rows }: { rows: PairedComparison[] }) {
  const W = 280
  const extent = Math.max(0.1, ...rows.flatMap((r) => [Math.abs(r.ci95?.[0] ?? 0), Math.abs(r.ci95?.[1] ?? 0), Math.abs(r.diff ?? 0)]))
  const x = (v: number) => W / 2 + (v / extent) * (W / 2 - 8)
  const isRate = (r: PairedComparison) => (r.metric ?? '').includes('rate')
  const hasP = rows.some((r) => r.mcnemar_p !== undefined)
  const hasRatio = rows.some((r) => r.token_ratio !== undefined)
  const fmt = (r: PairedComparison, v?: number) =>
    v === undefined ? '–' : isRate(r) ? `${v >= 0 ? '+' : ''}${(v * 100).toFixed(0)} pp` : `${v >= 0 ? '+' : ''}${v.toFixed(2)}`

  return (
    <table className="table table-compact" data-testid="paired-table">
      <thead>
        <tr>
          <th>Comparison</th>
          <th>Metric</th>
          <th className="num">Diff</th>
          <th>95% CI</th>
          <th className="center">Interval (dashed line = 0)</th>
          <th className="num">W / L / T</th>
          {hasP && <th className="num">McNemar p</th>}
          {hasRatio && <th className="num">Token ratio (95% CI)</th>}
        </tr>
      </thead>
      <tbody>
        {rows.map((r, i) => {
          const lo = r.ci95?.[0]
          const hi = r.ci95?.[1]
          // Significant if the whole interval sits on one side of zero.
          const tone = lo !== undefined && hi !== undefined ? (lo > 0 ? 'pos' : hi < 0 ? 'neg' : 'flat') : 'flat'
          return (
            <tr key={i}>
              <td>
                <strong>{r.comparison ?? '–'}</strong>{' '}
                {r.primary && (
                  <span className="badge badge-violet badge-xs" title="The preregistered primary comparison">
                    primary
                  </span>
                )}
              </td>
              <td className="muted">{r.metric ?? '–'}</td>
              <td className="num">{fmt(r, r.diff)}</td>
              <td className="muted small">
                [{fmt(r, lo)}, {fmt(r, hi)}]
              </td>
              <td className="center">
                <svg width={W} height={20} className={`forest forest-${tone}`} aria-hidden="true">
                  <line x1={x(0)} x2={x(0)} y1={0} y2={20} className="forest-zero" />
                  {lo !== undefined && hi !== undefined && <line x1={x(lo)} x2={x(hi)} y1={10} y2={10} className="forest-ci" />}
                  {r.diff !== undefined && <circle cx={x(r.diff)} cy={10} r={4.5} className="forest-dot" />}
                </svg>
              </td>
              <td className="num">
                {r.wins ?? '–'} / {r.losses ?? '–'} / {r.ties ?? '–'}
              </td>
              {hasP && <td className="num">{r.mcnemar_p?.toFixed(3) ?? '–'}</td>}
              {hasRatio && (
                <td className="num">
                  {r.token_ratio !== undefined ? `${r.token_ratio.toFixed(2)}×` : '–'}
                  {r.token_ratio_ci95 && (
                    <span className="muted small">
                      {' '}
                      [{r.token_ratio_ci95[0].toFixed(2)}, {r.token_ratio_ci95[1].toFixed(2)}]
                    </span>
                  )}
                </td>
              )}
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}
