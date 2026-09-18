// Renders the model's in-flight text delta at the foot of the transcript,
// in the same left-aligned plain-text container a persisted `text` block
// occupies (Transcript.tsx renders this right after the virtualised list, in
// flow) so there is no flicker or layout jump when the complete block
// replaces it. Deliberately plain text, not markdown: re-parsing and
// re-sanitising the whole buffer on every delta (many times a second) would
// be wasted work for text that is about to be replaced anyway.
import { usePartialsStore } from '../../store/partials'

export function PartialText({ sessionId }: { sessionId: string }) {
  const text = usePartialsStore((s) => s.bySession[sessionId])
  if (!text) return null

  return (
    <div className="max-w-[72ch] whitespace-pre-wrap text-[13px] leading-6 text-fg-primary">
      {text}
      <span
        aria-hidden
        className="ml-0.5! inline-block h-3.5 w-1.5 translate-y-0.5 animate-pulse bg-accent motion-reduce:animate-none"
      />
    </div>
  )
}
