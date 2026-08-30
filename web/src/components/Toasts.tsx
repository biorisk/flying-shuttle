import { useUiStore } from "../stores/uiStore";

/** Fixed-position stack of transient notifications (mostly failed requests). */
export default function Toasts() {
  const toasts = useUiStore((s) => s.toasts);
  const dismiss = useUiStore((s) => s.dismiss);

  if (toasts.length === 0) return null;

  return (
    <div className="toast-stack" role="status" aria-live="polite">
      {toasts.map((t) => (
        <div key={t.id} className={`toast toast--${t.kind}`}>
          <span className="toast-message">{t.message}</span>
          <button
            className="toast-dismiss"
            onClick={() => dismiss(t.id)}
            aria-label="Dismiss"
          >
            &times;
          </button>
        </div>
      ))}
    </div>
  );
}
