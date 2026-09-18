import { useToastStore, type ToastItem } from '../store/toast'

/** Fire-and-forget toast queue. Push once per user-visible event; the
 * Toaster (components/ui/Toast.tsx, mounted once in Shell.tsx) auto-dismisses
 * after a few seconds and the person can dismiss it early too. */
export function useToast() {
  const push = useToastStore((s) => s.push)
  return { toast: (item: Omit<ToastItem, 'id'>) => push(item) }
}
