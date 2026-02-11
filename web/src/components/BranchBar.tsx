import { useCallback, useEffect, useState } from "react";
import { useBranchStore } from "../stores/branchStore";
import { useOutlineStore } from "../stores/outlineStore";
import { useThreadStore } from "../stores/threadStore";

export default function BranchBar() {
  const {
    branches,
    activeBranch,
    switching,
    creating,
    comparing,
    compareBranchId,
    fetchBranches,
    createBranch,
    switchBranch,
    deleteBranch,
    loadBranchForCompare,
    clearCompare,
  } = useBranchStore();
  const fetchOutline = useOutlineStore((s) => s.fetchOutline);
  const diffActive = useOutlineStore((s) => s.diffActive);
  const computeDiff = useOutlineStore((s) => s.computeDiff);
  const clearDiff = useOutlineStore((s) => s.clearDiff);
  const fetchThreads = useThreadStore((s) => s.fetchThreads);

  const [showSplitInput, setShowSplitInput] = useState(false);
  const [branchName, setBranchName] = useState("");
  const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);

  useEffect(() => {
    fetchBranches();
  }, [fetchBranches]);

  const handleSplit = useCallback(async () => {
    const name = branchName.trim() || "untitled";
    const result = await createBranch(name);
    if (result) {
      await fetchOutline();
      await fetchThreads();
    }
    setBranchName("");
    setShowSplitInput(false);
  }, [branchName, createBranch, fetchOutline, fetchThreads]);

  const handleSwitch = useCallback(
    async (id: string) => {
      if (diffActive) clearDiff();
      const ok = await switchBranch(id);
      if (ok) {
        await fetchOutline();
        await fetchThreads();
      }
    },
    [switchBranch, fetchOutline, fetchThreads, diffActive, clearDiff],
  );

  const handleDelete = useCallback(
    async (id: string) => {
      if (confirmDeleteId !== id) {
        setConfirmDeleteId(id);
        return;
      }
      if (diffActive && compareBranchId === id) clearDiff();
      await deleteBranch(id);
      setConfirmDeleteId(null);
    },
    [confirmDeleteId, deleteBranch, diffActive, compareBranchId, clearDiff],
  );

  const handleCompare = useCallback(
    async (id: string) => {
      const data = await loadBranchForCompare(id);
      if (data) computeDiff(data);
    },
    [loadBranchForCompare, computeDiff],
  );

  const handleClearCompare = useCallback(() => {
    clearCompare();
    clearDiff();
  }, [clearCompare, clearDiff]);

  const otherBranches = branches.filter((b) => !b.active);

  // No branches exist yet: show only Split button.
  if (branches.length === 0) {
    return (
      <div className="branch-bar">
        {!showSplitInput ? (
          <button
            className="branch-split-btn"
            onClick={() => setShowSplitInput(true)}
            disabled={creating}
          >
            {creating ? "..." : "Split"}
          </button>
        ) : (
          <span className="branch-split-inline">
            <input
              className="branch-name-input"
              type="text"
              value={branchName}
              onChange={(e) => setBranchName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") handleSplit();
                if (e.key === "Escape") {
                  setShowSplitInput(false);
                  setBranchName("");
                }
              }}
              placeholder="Branch name..."
              autoFocus
            />
            <button className="branch-split-confirm" onClick={handleSplit}>
              &#x2713;
            </button>
          </span>
        )}
      </div>
    );
  }

  return (
    <div className="branch-bar">
      <span className="branch-bar-label">Branch:</span>
      {activeBranch && (
        <span className="branch-active-name">{activeBranch.name}</span>
      )}

      {otherBranches.map((b) => (
        <span key={b.id} className="branch-chip">
          <button
            className="branch-chip-name"
            onClick={() => handleSwitch(b.id)}
            disabled={switching}
            title={`Switch to "${b.name}"`}
          >
            {b.name}
          </button>
          {!(diffActive && compareBranchId === b.id) ? (
            <button
              className="branch-compare-btn"
              onClick={() => handleCompare(b.id)}
              disabled={comparing}
              title={`Compare with "${b.name}"`}
            >
              {comparing ? "..." : "Diff"}
            </button>
          ) : (
            <button
              className="branch-clear-compare"
              onClick={handleClearCompare}
              title="Clear diff"
            >
              Clear
            </button>
          )}
          <button
            className="branch-delete-btn"
            onClick={() => handleDelete(b.id)}
            title={
              confirmDeleteId === b.id
                ? "Click again to confirm"
                : `Delete "${b.name}"`
            }
          >
            {confirmDeleteId === b.id ? "Sure?" : "\u2715"}
          </button>
        </span>
      ))}

      {!showSplitInput ? (
        <button
          className="branch-split-btn"
          onClick={() => setShowSplitInput(true)}
          disabled={creating}
        >
          {creating ? "..." : "+ Split"}
        </button>
      ) : (
        <span className="branch-split-inline">
          <input
            className="branch-name-input"
            type="text"
            value={branchName}
            onChange={(e) => setBranchName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") handleSplit();
              if (e.key === "Escape") {
                setShowSplitInput(false);
                setBranchName("");
              }
            }}
            placeholder="Branch name..."
            autoFocus
          />
          <button className="branch-split-confirm" onClick={handleSplit}>
            &#x2713;
          </button>
        </span>
      )}
    </div>
  );
}
