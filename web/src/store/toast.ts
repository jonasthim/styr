// Toast queue (zustand, same pattern as store/ui.ts): components call
// useToast().push(...) from anywhere; <Toaster/> (components/ui/Toast.tsx),
// mounted once in Shell.tsx, is the only thing that reads this store.
import { create } from 'zustand'

export interface ToastItem {
  id: string
  title: string
  description?: string
  tone?: 'neutral' | 'success' | 'danger'
}

interface ToastState {
  toasts: ToastItem[]
  push: (toast: Omit<ToastItem, 'id'>) => void
  dismiss: (id: string) => void
}

export const useToastStore = create<ToastState>((set) => ({
  toasts: [],
  push: (toast) =>
    set((state) => ({
      toasts: [...state.toasts, { ...toast, id: crypto.randomUUID() }],
    })),
  dismiss: (id) => set((state) => ({ toasts: state.toasts.filter((t) => t.id !== id) })),
}))
