import { useCallback, useEffect, useState } from "react";
import { useThreadStore } from "../stores/threadStore";
import { useOutlineStore } from "../stores/outlineStore";

export default function ThreadSelector() {
  const {
    threads,
    selected,
    threadNodeIds,
    brushMode,
    fetchThreads,
    selectThread,
    createThread,
    deleteThread,
    setBrushMode,
  } = useThreadStore();
  const allNodes = useOutlineStore((s) => s.allNodes);
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState("");

  useEffect(() => {
    fetchThreads();
  }, [fetchThreads]);

  const handleSelect = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      const val = e.target.value;
      selectThread(val || null);
    },
    [selectThread]
  );

  const handleCreate = useCallback(async () => {
    if (!newName.trim()) return;
    const thread = await createThread({ name: newName.trim() });
    if (thread) {
      await selectThread(thread.id);
    }
    setNewName("");
    setCreating(false);
  }, [newName, createThread, selectThread]);

  const handleDelete = useCallback(async () => {
    if (!selected) return;
    await deleteThread(selected.id);
  }, [selected, deleteThread]);

  const outlineNodeCount = allNodes.filter((n) => n.type === "outline").length;
  const activeCount = threadNodeIds.size;

  return (
    <div className="thread-selector">
      <label className="thread-selector-label">Thread:</label>
      <select
        className="thread-selector-dropdown"
        value={selected?.id ?? ""}
        onChange={handleSelect}
      >
        <option value="">All nodes</option>
        {threads.map((t) => (
          <option key={t.id} value={t.id}>
            {t.name}
          </option>
        ))}
      </select>

      {selected && outlineNodeCount > 0 && (
        <span className="thread-coverage">
          {activeCount} of {outlineNodeCount} nodes
        </span>
      )}

      {!creating ? (
        <button
          className="thread-create-btn"
          onClick={() => setCreating(true)}
          title="Create new thread"
        >
          +
        </button>
      ) : (
        <span className="thread-create-inline">
          <input
            className="thread-create-input"
            type="text"
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") handleCreate();
              if (e.key === "Escape") {
                setCreating(false);
                setNewName("");
              }
            }}
            placeholder="Thread name..."
            autoFocus
          />
          <button className="thread-create-confirm" onClick={handleCreate}>
            &#x2713;
          </button>
        </span>
      )}

      {selected && (
        <button
          className={`thread-brush-btn ${brushMode ? "active" : ""}`}
          onClick={() => setBrushMode(!brushMode)}
          title={brushMode ? "Exit brush mode" : "Paint thread path"}
        >
          &#x1F58C;
        </button>
      )}

      {selected && (
        <button
          className="thread-delete-btn"
          onClick={handleDelete}
          title="Delete thread"
        >
          &#x2715;
        </button>
      )}
    </div>
  );
}
