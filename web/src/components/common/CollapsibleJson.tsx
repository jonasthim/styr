// A `<summary>`-triggered, collapsed-by-default JSON block, styled like
// ToolInput's `<pre>` blocks. Used for a delivery's raw payload wherever it
// would otherwise dominate a run/deliveries view (plan: "the delivery
// payload collapsed").
export function CollapsibleJson({ label, value, defaultOpen = false }: { label: string; value: unknown; defaultOpen?: boolean }) {
  return (
    <details data-testid="collapsible-json" open={defaultOpen} className="group rounded-[var(--radius-control)] border border-hairline">
      <summary className="flex cursor-pointer list-none items-center gap-1.5 px-3 py-2 text-[12px] font-medium text-fg-secondary marker:content-none hover:text-fg-primary">
        <span aria-hidden className="inline-block transition-transform duration-[var(--duration-fast)] group-open:rotate-90">
          ›
        </span>
        {label}
      </summary>
      <pre className="overflow-x-auto border-t border-hairline bg-surface-3 px-3 py-2 font-mono text-[12px] leading-5 text-fg-primary">
        <code>{JSON.stringify(value, null, 2)}</code>
      </pre>
    </details>
  )
}
