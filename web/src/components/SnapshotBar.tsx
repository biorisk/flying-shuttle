import { useCallback, useEffect, useState } from "react";
import { useSnapshotStore } from "../stores/snapshotStore";
import { useOutlineStore } from "../stores/outlineStore";
import { useThreadStore } from "../stores/threadStore";

export default function SnapshotBar() {
  const {
    snapshots,
    saving,
    restoring,
    fetchSnapshots,
    createSnapshot,
    deleteSnapshot,
    restoreSnapshot,
  } = useSnapshotStore();
  const fetchOutline = useOutlineStore((s) => s.fetchOutline);
  const fetchThreads = useThreadStore((s) => s.fetchThreads);

  const [selectedId, setSelectedId] = useState("");
  const [showSaveInput, setShowSaveInput] = useState(false);
  const [label, setLabel] = useState("");
  const [confirmRestore, setConfirmRestore] = useState(false);

  useEffect(() => {
    fetchSnapshots();
  }, [fetchSnapshots]);

  const handleSave = useCallback(async () => {
    const snap = await createSnapshot(label.trim());
    if (snap) {
      setSelectedId(snap.id);
    }
    setLabel("");
    setShowSaveInput(false);
  }, [label, createSnapshot]);

  const handleRestore = useCallback(async () => {
    if (!confirmRestore) {
      setConfirmRestore(true);
      return;
    }
    const ok = await restoreSnapshot(selectedId);
    setConfirmRestore(false);
    if (ok) {
      fetchOutline();
      fetchThreads();
    }
  }, [confirmRestore, selectedId, restoreSnapshot, fetchOutline, fetchThreads]);

  const handleDelete = useCallback(async () => {
    if (!selectedId) return;
    await deleteSnapshot(selectedId);
    setSelectedId("");
  }, [selectedId, deleteSnapshot]);

  const formatDate = (iso: string) => {
    const d = new Date(iso);
    return d.toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  };

  return (
    <div className="snapshot-bar">
      <label className="snapshot-bar-label">Snapshot:</label>

      <select
        className="snapshot-bar-dropdown"
        value={selectedId}
        onChange={(e) => {
          setSelectedId(e.target.value);
          setConfirmRestore(false);
        }}
      >
        <option value="">Select...</option>
        {snapshots.map((s) => (
          <option key={s.id} value={s.id}>
            {s.label || "Untitled"} — {formatDate(s.created_at)}
          </option>
        ))}
      </select>

      {!showSaveInput ? (
        <button
          className="snapshot-save-btn"
          onClick={() => setShowSaveInput(true)}
          disabled={saving}
          title="Save snapshot"
        >
          {saving ? "..." : "Save"}
        </button>
      ) : (
        <span className="snapshot-save-inline">
          <input
            className="snapshot-save-input"
            type="text"
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") handleSave();
              if (e.key === "Escape") {
                setShowSaveInput(false);
                setLabel("");
              }
            }}
            placeholder="Label (optional)..."
            autoFocus
          />
          <button className="snapshot-save-confirm" onClick={handleSave}>
            &#x2713;
          </button>
        </span>
      )}

      {selectedId && (
        <>
          <button
            className={`snapshot-restore-btn ${confirmRestore ? "confirm" : ""}`}
            onClick={handleRestore}
            disabled={restoring}
          >
            {restoring ? "..." : confirmRestore ? "Confirm?" : "Restore"}
          </button>
          <button
            className="snapshot-delete-btn"
            onClick={handleDelete}
            title="Delete snapshot"
          >
            &#x2715;
          </button>
        </>
      )}
    </div>
  );
}
