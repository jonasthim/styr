// Value debounce, extracted from TemplateEditor.tsx's local copy so the cron
// preview and the template preview settle on the same behaviour: the latest
// value wins, and nothing fires until typing pauses.
import { useEffect, useState } from 'react'

export function useDebounced<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delayMs)
    return () => clearTimeout(timer)
  }, [value, delayMs])
  return debounced
}
