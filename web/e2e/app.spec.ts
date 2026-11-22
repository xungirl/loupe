import { test, expect } from '@playwright/test'

// End-to-end tests against the real Go server + freshly seeded demo DB
// (see e2e/start-server.sh). The demo has 120 corpus traces with injected
// faults, 4 Claude Code sessions, and a live simulator.

test('sessions list loads and filters by "has findings"', async ({ page }) => {
  await page.goto('/')
  const rows = page.getByTestId('trace-row')
  await expect(rows.first()).toBeVisible()
  await expect(page.getByTestId('health')).toContainText('server ok')

  const totalBefore = await page.getByTestId('pager-label').innerText()
  // click() rather than check(): the checkbox is controlled by the URL, which
  // React Router updates asynchronously, so its state flips a tick later.
  await page.getByTestId('filter-has-finding').click()
  await expect(page.getByTestId('filter-has-finding')).toBeChecked()
  await expect(page).toHaveURL(/has_finding=true/)
  await expect(page.getByTestId('pager-label')).not.toHaveText(totalBefore)

  // Every remaining row shows at least one detector badge.
  await expect(rows.first()).toBeVisible()
  const count = await rows.count()
  expect(count).toBeGreaterThan(0)
  for (let i = 0; i < Math.min(count, 10); i++) {
    await expect(rows.nth(i).locator('[data-testid=finding-badges] .badge').first()).toBeVisible()
  }
})

test('sessions search narrows to a single trace', async ({ page }) => {
  await page.goto('/')
  await page.getByLabel('Search traces').fill('cc-sess-new')
  await expect(page).toHaveURL(/q=cc-sess-new/)
  await expect(page.getByTestId('trace-row')).toHaveCount(1)
})

test('opening a trace renders the waterfall and the span panel', async ({ page }) => {
  await page.goto('/?source=corpus')
  await page.getByTestId('trace-row').first().click()
  await expect(page).toHaveURL(/\/traces\/corpus-/)
  const wfRows = page.getByTestId('wf-row')
  await expect(wfRows.first()).toBeVisible()
  expect(await wfRows.count()).toBeGreaterThan(10)

  // Select a row → side panel with span fields.
  await wfRows.nth(2).click()
  await expect(page.getByTestId('span-panel')).toBeVisible()
  await expect(page.getByTestId('span-panel')).toContainText('Span ID')

  // Collapse the root: only the root row stays.
  await wfRows.first().getByRole('button', { name: 'Collapse' }).click()
  await expect(wfRows).toHaveCount(1)
})

test('agent tree renders the nested Claude Code session', async ({ page }) => {
  await page.goto('/traces/cc-sess-new')
  await page.getByRole('tab', { name: /Agent tree/ }).click()
  const nodes = page.getByTestId('tree-node')
  await expect(nodes).toHaveCount(3)
  for (const name of ['claude-code', 'code-reviewer', 'Explore']) {
    await expect(page.getByTestId('agent-tree')).toContainText(name)
  }
  // Clicking a node filters the waterfall to that agent.
  await nodes.filter({ hasText: 'Explore' }).click()
  await expect(page).toHaveURL(/agent=exp2/)
  await expect(page.getByTestId('waterfall')).toBeVisible()
  await expect(page.getByTestId('wf-row')).toHaveCount(5)
})

test('findings page links to a trace with the finding highlighted', async ({ page }) => {
  await page.goto('/findings')
  await expect(page.getByTestId('finding-group').first()).toBeVisible()
  await page.getByTestId('det-card-rework').click()
  await expect(page).toHaveURL(/detector=rework/)
  await page.getByTestId('finding-link').first().click()
  await expect(page).toHaveURL(/\/traces\/.+\?finding=\d+/)
  await expect(page.locator('.wf-row.is-highlighted').first()).toBeVisible()
  await expect(page.locator('[data-testid=finding-chip].chip-active')).toBeVisible()
})

test('live page receives spans from the demo simulator', async ({ page }) => {
  await page.goto('/live')
  await expect(page.getByTestId('live-status')).toHaveText('connected')
  await expect(page.getByTestId('live-row').first()).toBeVisible({ timeout: 20_000 })
})

test('insights renders charts and tables', async ({ page }) => {
  await page.goto('/insights')
  await expect(page.getByTestId('chart-bar').first()).toBeVisible()
  expect(await page.getByTestId('chart-bar').count()).toBeGreaterThan(3)
  await expect(page.getByTestId('detector-bars')).toContainText('loop')
  await expect(page.getByTestId('tools-table')).toContainText('Bash')
  await expect(page.getByTestId('hotspots-table')).toContainText('src/')
})

test('journey renders every stage', async ({ page }) => {
  await page.goto('/journey')
  await expect(page.getByTestId('journey-stage')).toHaveCount(13)
  await expect(page.locator('.stage-todo')).toHaveCount(3)
  // Scrolling to the end reveals the last card (IntersectionObserver).
  await page.getByTestId('journey-stage').last().scrollIntoViewIfNeeded()
  await expect(page.getByTestId('journey-stage').last()).toHaveClass(/is-visible/)
})

test('experiments shows the empty state or a report (with banner if dry run)', async ({ page, request }) => {
  const { reports } = await (await request.get('/api/v1/experiments')).json()
  await page.goto('/experiments')
  if (!reports || reports.length === 0) {
    await expect(page.getByTestId('experiments-empty')).toContainText('make experiment-dryrun')
    return
  }
  await expect(page.getByTestId('experiment-report')).toBeVisible()
  const first = reports[0]
  const banner = page.getByTestId('dry-run-banner')
  if (first.provider === 'fake' || first.is_dry_run) await expect(banner).toBeVisible()
  else await expect(banner).toHaveCount(0)
})

test('unknown trace shows a not-found state', async ({ page }) => {
  await page.goto('/traces/does-not-exist')
  await expect(page.getByText('Trace not found')).toBeVisible()
})
