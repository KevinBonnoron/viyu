import { type DependencyList, useEffect, useRef } from 'react';

/**
 * Runs `task` with an `AbortSignal` after the render commits and aborts the signal when `deps` change or the component
 * unmounts. The task starts on the next tick, so an immediate cleanup (React StrictMode, a rapid dependency change)
 * sends no request at all. `deps` is what restarts the task: `task` always runs with the closure of the latest render.
 * A task that sets up something to tear down (a repeating timer) returns the function that does it; it runs, just before
 * the signal aborts, when the task is superseded or the component unmounts.
 */
export function useAbortableEffect(task: (signal: AbortSignal) => undefined | Promise<unknown> | (() => void), deps: DependencyList): void {
  const latest = useRef(task);
  latest.current = task;
  useEffect(
    () => {
      const controller = new AbortController();
      let teardown: (() => void) | undefined;
      const start = window.setTimeout(() => {
        const result = latest.current(controller.signal);
        if (typeof result === 'function') {
          teardown = result;
        }
      }, 0);
      return () => {
        window.clearTimeout(start);
        teardown?.();
        controller.abort();
      };
    },
    // biome-ignore lint/correctness/useExhaustiveDependencies: the caller lists what restarts the task; `latest` never changes.
    deps,
  );
}
