import { useEffect, useRef, useState } from 'react'
import { journey } from '../journey'
import { PageHeader } from '../components/ui'

/**
 * Scroll-animated roadmap.
 *
 * Two effects drive the animation:
 *  1. A scroll listener computes how far the timeline has scrolled past a
 *     "reading line" 60% down the viewport, and fills the gradient line to
 *     that point (progress 0 → 1).
 *  2. An IntersectionObserver adds `is-visible` to each stage the first time it
 *     enters the viewport; CSS fades and slides the card in.
 */
export default function JourneyPage() {
  const timelineRef = useRef<HTMLOListElement>(null)
  const [progress, setProgress] = useState(0)
  const [visible, setVisible] = useState<Set<number>>(new Set())
  const done = journey.filter((s) => s.status === 'done').length

  useEffect(() => {
    function update() {
      const el = timelineRef.current
      if (!el) return
      const rect = el.getBoundingClientRect()
      const readingLine = window.innerHeight * 0.6
      // 0 when the timeline's top is at the reading line, 1 when its bottom is.
      const p = rect.height > 0 ? (readingLine - rect.top) / rect.height : 0
      setProgress(Math.max(0, Math.min(1, p)))
    }
    update()
    window.addEventListener('scroll', update, { passive: true })
    window.addEventListener('resize', update)
    return () => {
      window.removeEventListener('scroll', update)
      window.removeEventListener('resize', update)
    }
  }, [])

  useEffect(() => {
    const items = Array.from(timelineRef.current?.querySelectorAll<HTMLElement>('[data-index]') ?? [])
    // Old browsers / test environments without IntersectionObserver: show everything.
    if (typeof IntersectionObserver === 'undefined') {
      setVisible(new Set(items.map((_, i) => i)))
      return
    }
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (!e.isIntersecting) continue
          const i = Number((e.target as HTMLElement).dataset.index)
          setVisible((prev) => new Set(prev).add(i))
          io.unobserve(e.target) // reveal once; don't hide again on scroll-up
        }
      },
      { threshold: 0.15, rootMargin: '0px 0px -8% 0px' },
    )
    items.forEach((el) => io.observe(el))
    return () => io.disconnect()
  }, [])

  return (
    <div className="page page-journey">
      <PageHeader
        title="Journey"
        subtitle="How Loupe was built, stage by stage — and what comes next."
        actions={
          <div className="journey-score">
            <strong>
              {done} / {journey.length}
            </strong>{' '}
            stages done
            <span className="progress">
              <span className="progress-fill gradient" style={{ width: `${(done / journey.length) * 100}%` }} />
            </span>
          </div>
        }
      />

      <ol className="timeline" ref={timelineRef} data-testid="timeline">
        {/* The gray track and the gradient fill sit behind the dots. */}
        <span className="timeline-track" aria-hidden="true" />
        <span className="timeline-fill" aria-hidden="true" style={{ height: `${progress * 100}%` }} />

        {journey.map((stage, i) => (
          <li
            key={stage.title}
            data-index={i}
            data-testid="journey-stage"
            className={`stage stage-${stage.status} ${visible.has(i) ? 'is-visible' : ''}`}
          >
            <span className="stage-dot" aria-hidden="true">
              {stage.status === 'done' ? i + 1 : ''}
            </span>
            <div className="stage-card">
              <div className="stage-head">
                <span className="stage-num">{stage.status === 'done' ? `Stage ${i + 1}` : 'Next'}</span>
                <span className={`badge ${stage.status === 'done' ? 'badge-green' : 'badge-neutral'}`}>
                  {stage.status === 'done' ? 'done' : 'todo'}
                </span>
              </div>
              <h3>{stage.title}</h3>
              <p>{stage.description}</p>
              {stage.facts.length > 0 && (
                <ul className="stage-facts">
                  {stage.facts.map((f) => (
                    <li key={f}>{f}</li>
                  ))}
                </ul>
              )}
            </div>
          </li>
        ))}
      </ol>
    </div>
  )
}
