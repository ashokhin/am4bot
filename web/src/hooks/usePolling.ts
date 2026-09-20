import { useEffect, useRef } from 'react'

// 20s -- roughly matches a typical Prometheus scrape interval; polling
// faster wouldn't surface new data any sooner, since the underlying
// gauges themselves only update that often.
const POLL_INTERVAL_MS = 20_000

/**
 * Calls fn immediately, then every POLL_INTERVAL_MS while the tab is
 * visible -- paused via the Page Visibility API while hidden (no point
 * hammering the API for a tab nobody's looking at), and immediately
 * refetched the moment it becomes visible again so switching back never
 * shows stale data for a full interval.
 *
 * This is a polling-based "auto refresh", the same model Grafana's own
 * dashboards use by default -- not a WebSocket. The underlying data
 * source (Prometheus) is itself pull-based with no push/subscribe API, so
 * a WebSocket here would just be this same polling wrapped in a
 * persistent connection (reconnect handling, backpressure, auth-on-
 * handshake, ...) without enough benefit at this project's scale to
 * justify the added complexity.
 *
 * deps works like useEffect's own dependency list -- pass whatever fn
 * itself closes over that should restart polling from scratch (e.g. a
 * selected user's uuid).
 */
export function usePolling(fn: () => void, deps: React.DependencyList) {
  // Always call the LATEST fn, even though the interval itself is only
  // ever (re)created when deps change -- avoids either a stale closure
  // or restarting the timer (and losing its phase) on every render.
  const fnRef = useRef(fn)
  fnRef.current = fn

  useEffect(() => {
    let intervalId: ReturnType<typeof setInterval> | undefined

    function start() {
      if (intervalId !== undefined) return
      intervalId = setInterval(() => fnRef.current(), POLL_INTERVAL_MS)
    }

    function stop() {
      clearInterval(intervalId)
      intervalId = undefined
    }

    function handleVisibilityChange() {
      if (document.hidden) {
        stop()
      } else {
        fnRef.current()
        start()
      }
    }

    fnRef.current()

    if (!document.hidden) {
      start()
    }

    document.addEventListener('visibilitychange', handleVisibilityChange)

    return () => {
      stop()
      document.removeEventListener('visibilitychange', handleVisibilityChange)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- deps is the caller's own explicit list, not fn itself (see fnRef above)
  }, deps)
}
