// Measures an element's content width and keeps it current, so a chart can
// lay itself out in real pixels instead of scaling a fixed viewBox (which
// would shrink its labels along with it). Returns 0 until the first
// observation, which every caller treats as "not ready to draw yet".
import { useEffect, useRef, useState, type RefObject } from 'react'

export function useElementWidth<T extends HTMLElement>(): [RefObject<T | null>, number] {
  const ref = useRef<T | null>(null)
  const [width, setWidth] = useState(0)

  useEffect(() => {
    const node = ref.current
    if (!node || typeof ResizeObserver === 'undefined') return
    setWidth(node.clientWidth)
    const observer = new ResizeObserver((entries) => {
      const entry = entries[0]
      if (entry) setWidth(Math.round(entry.contentRect.width))
    })
    observer.observe(node)
    return () => observer.disconnect()
  }, [])

  return [ref, width]
}
