import { useCallback, useEffect, useState } from 'react'

export interface ApiState<T> {
  data: T | undefined
  error: Error | undefined
  loading: boolean
  /** Fetch again with the same arguments (e.g. after a POST). */
  reload: () => void
}

/**
 * Runs an async loader whenever `deps` change and tracks loading/error state.
 *
 * Example:  const { data, loading } = useApi(() => api.getTrace(id), [id])
 *
 * The `cancelled` flag ignores responses from an older request that finishes
 * after a newer one started (e.g. the user typed quickly in the search box).
 */
export function useApi<T>(loader: () => Promise<T>, deps: unknown[]): ApiState<T> {
  const [data, setData] = useState<T>()
  const [error, setError] = useState<Error>()
  const [loading, setLoading] = useState(true)
  const [version, setVersion] = useState(0)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    loader()
      .then((d) => {
        if (!cancelled) {
          setData(d)
          setError(undefined)
        }
      })
      .catch((e: unknown) => {
        if (!cancelled) setError(e instanceof Error ? e : new Error(String(e)))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
    // The caller lists the real dependencies; `loader` is a new function on
    // every render, so including it would refetch forever.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, version])

  const reload = useCallback(() => setVersion((v) => v + 1), [])
  return { data, error, loading, reload }
}
