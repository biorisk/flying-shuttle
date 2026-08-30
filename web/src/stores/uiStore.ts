import { create } from "zustand";

export type ToastKind = "error" | "info" | "success";

export interface Toast {
  id: number;
  kind: ToastKind;
  message: string;
}

interface UiState {
  toasts: Toast[];
  notify: (message: string, kind?: ToastKind) => void;
  dismiss: (id: number) => void;
}

let seq = 0;
const TTL_MS = 6000;

export const useUiStore = create<UiState>((set) => ({
  toasts: [],
  notify: (message, kind = "error") => {
    const id = ++seq;
    set((s) => {
      // Collapse an identical message already on screen.
      if (s.toasts.some((t) => t.message === message)) return s;
      return { toasts: [...s.toasts, { id, kind, message }] };
    });
    setTimeout(() => {
      set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }));
    }, TTL_MS);
  },
  dismiss: (id) => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
}));
