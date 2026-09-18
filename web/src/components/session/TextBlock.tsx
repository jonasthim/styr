// Renders a complete assistant text block as sanitised markdown. Code blocks
// get a copy button in their header. Uses an isolated `Marked` instance (not
// the global `marked` singleton) so this component's renderer customisation
// never leaks into anything else that might import marked later.
import { useMemo, useRef } from 'react'
import { Marked } from 'marked'
import DOMPurify from 'dompurify'

function escapeHtml(text: string): string {
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}

const md = new Marked({
  gfm: true,
  breaks: true,
  renderer: {
    code({ text, lang }) {
      const language = (lang ?? '').split(/\s+/)[0]
      return (
        `<div class="my-2! overflow-hidden rounded-[var(--radius-1)] border border-hairline bg-surface-2" data-tb-code>` +
        `<div class="flex items-center justify-between border-b border-hairline px-2! py-1!">` +
        `<span class="font-mono text-[11px] text-fg-muted">${escapeHtml(language || 'text')}</span>` +
        `<button type="button" class="rounded px-1.5! py-0.5! font-mono text-[11px] text-fg-secondary transition-colors duration-150 hover:bg-surface-3 hover:text-fg-primary" data-tb-copy aria-label="Copy code">Copy</button>` +
        `</div>` +
        `<pre class="overflow-x-auto p-2! font-mono text-[12px] leading-5 text-fg-primary"><code>${escapeHtml(text)}</code></pre></div>`
      )
    },
  },
})

function renderMarkdown(text: string): string {
  const html = md.parse(text, { async: false }) as string
  return DOMPurify.sanitize(html, { ADD_ATTR: ['data-tb-copy', 'data-tb-code'] })
}

export function TextBlock({ text }: { text: string }) {
  const html = useMemo(() => renderMarkdown(text), [text])
  const rootRef = useRef<HTMLDivElement>(null)

  function onClick(e: React.MouseEvent<HTMLDivElement>) {
    const button = (e.target as HTMLElement).closest<HTMLButtonElement>('[data-tb-copy]')
    if (!button) return
    const code = button.closest('[data-tb-code]')?.querySelector('code')
    if (!code) return
    void navigator.clipboard.writeText(code.textContent ?? '')
    const original = button.textContent
    button.textContent = 'Copied'
    setTimeout(() => {
      if (button.isConnected) button.textContent = original
    }, 1200)
  }

  return (
    <div
      ref={rootRef}
      className="tb-markdown max-w-[72ch] text-[13px] leading-6 text-fg-primary [&_a]:text-accent [&_a]:underline [&_code]:font-mono [&_code]:text-[12px] [&_h1]:text-[15px] [&_h1]:font-semibold [&_h2]:text-[14px] [&_h2]:font-semibold [&_li]:my-0.5! [&_ol]:my-1.5! [&_ol]:pl-5! [&_p+p]:mt-2! [&_p]:my-0! [&_ul]:my-1.5! [&_ul]:pl-5!"
      onClick={onClick}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  )
}
