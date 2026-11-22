import { test, expect, type Page } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// `npm run screenshots` → docs/screenshots/*.png at 1440×900, light theme.
const OUT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../docs/screenshots')
const shot = (page: Page, name: string) => page.screenshot({ path: path.join(OUT, name) })

test.describe.configure({ mode: 'serial' })

test('sessions', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByTestId('trace-row').first()).toBeVisible()
  await expect(page.locator('[data-testid=finding-badges] .det').first()).toBeVisible()
  await shot(page, 'sessions.png')
})

test('trace waterfall with a highlighted finding', async ({ page, request }) => {
  // A corpus trace with a rework finding: its edit spans get highlighted.
  const res = await request.get('/api/v1/findings?detector=rework&limit=50')
  const { findings } = await res.json()
  const f = findings.find((x: { trace_id: string }) => x.trace_id.startsWith('corpus-')) ?? findings[0]
  await page.goto(`/traces/${f.trace_id}?finding=${f.id}`)
  const highlighted = page.locator('.wf-row.is-highlighted')
  await expect(highlighted.first()).toBeVisible()
  await highlighted.nth(1).click()
  await expect(page.getByTestId('span-panel')).toBeVisible()
  await page.waitForTimeout(600) // let the smooth scroll settle
  await shot(page, 'trace-waterfall.png')
})

test('agent tree', async ({ page }) => {
  await page.goto('/traces/cc-sess-new?tab=agents')
  await expect(page.getByTestId('tree-node')).toHaveCount(3)
  await shot(page, 'agent-tree.png')
})

test('live', async ({ page }) => {
  await page.goto('/live')
  await expect(page.getByTestId('live-row').nth(11)).toBeVisible({ timeout: 40_000 })
  await page.waitForTimeout(15_000) // let the spans/sec sparkline collect some history
  await shot(page, 'live.png')
})

test('insights', async ({ page }) => {
  await page.goto('/insights')
  await expect(page.getByTestId('chart-bar').first()).toBeVisible()
  await shot(page, 'insights.png')
})

test('findings', async ({ page }) => {
  await page.goto('/findings')
  await expect(page.getByTestId('finding-group').first()).toBeVisible()
  await shot(page, 'findings.png')
})

test('journey', async ({ page }) => {
  await page.goto('/journey')
  await expect(page.getByTestId('journey-stage')).toHaveCount(13)
  // Even unscrolled, the line is filled down to the "reading line" (60% of the
  // viewport); wait for the cards' fade-in transition to finish.
  await expect(page.getByTestId('journey-stage').nth(2)).toHaveClass(/is-visible/)
  await page.waitForTimeout(900)
  await shot(page, 'journey.png')
})

test('experiments', async ({ page }) => {
  await page.goto('/experiments')
  await expect(page.getByTestId('experiments-empty').or(page.getByTestId('experiment-report'))).toBeVisible()
  await shot(page, 'experiments.png')
})
