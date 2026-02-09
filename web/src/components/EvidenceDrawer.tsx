import { useCallback, useEffect, useState } from "react";
import { useNodeStore } from "../stores/nodeStore";
import { nodes as nodesApi } from "../services/api";
import type { Chunk } from "../types/model";

function formatTime(ms: number): string {
  const totalSec = Math.floor(ms / 1000);
  const min = Math.floor(totalSec / 60);
  const sec = totalSec % 60;
  return `${min}:${sec.toString().padStart(2, "0")}`;
}

type Fidelity = "high" | "medium" | "low";

function assessFidelity(chunk: Chunk, nodeTitle: string): Fidelity {
  if (!nodeTitle || !chunk.content) return "low";
  const title = nodeTitle.toLowerCase();
  const content = chunk.content.toLowerCase();

  // High fidelity: node title appears nearly verbatim in the chunk.
  if (content.includes(title)) return "high";

  // Check word overlap for medium fidelity.
  const titleWords = new Set(title.split(/\s+/).filter((w) => w.length > 3));
  if (titleWords.size === 0) return "low";
  let matches = 0;
  for (const word of titleWords) {
    if (content.includes(word)) matches++;
  }
  const overlap = matches / titleWords.size;
  if (overlap >= 0.5) return "medium";
  return "low";
}

const fidelityConfig: Record<Fidelity, { icon: string; label: string }> = {
  high: { icon: "\uD83C\uDFA4", label: "Near-exact quote" },
  medium: { icon: "\uD83D\uDD17", label: "Closely related" },
  low: { icon: "\u2728", label: "Inferred / synthesized" },
};

export default function EvidenceDrawer() {
  const { selected } = useNodeStore();
  const [chunks, setChunks] = useState<Chunk[]>([]);
  const [loading, setLoading] = useState(false);

  const fetchChunks = useCallback(async (nodeId: string) => {
    setLoading(true);
    try {
      const result = await nodesApi.getChunks(nodeId);
      setChunks(result ?? []);
    } catch {
      setChunks([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (selected) {
      fetchChunks(selected.id);
    } else {
      setChunks([]);
    }
  }, [selected, fetchChunks]);

  if (!selected) {
    return (
      <p className="pane-placeholder">
        Select a bullet to view its evidence and backing chunks.
      </p>
    );
  }

  return (
    <div className="evidence-drawer">
      <h3 className="evidence-title">{selected.title || "(untitled)"}</h3>

      <dl className="evidence-meta">
        <dt>Type</dt>
        <dd>{selected.type}</dd>
        <dt>Status</dt>
        <dd>{selected.locked ? "Locked" : "Draft"}</dd>
        {selected.version > 1 && (
          <>
            <dt>Version</dt>
            <dd>{selected.version}</dd>
          </>
        )}
      </dl>

      {selected.body && (
        <section className="evidence-body">
          <h4>Body</h4>
          <p>{selected.body}</p>
        </section>
      )}

      <section className="evidence-chunks">
        <h4>Backing Evidence ({chunks.length})</h4>
        {loading && <p className="evidence-loading">Loading chunks...</p>}
        {!loading && chunks.length === 0 && (
          <p className="evidence-empty">No chunks associated with this node.</p>
        )}
        {!loading &&
          chunks.map((chunk) => {
            const fidelity = assessFidelity(chunk, selected.title);
            const config = fidelityConfig[fidelity];
            return (
              <div key={chunk.id} className={`evidence-chunk fidelity-${fidelity}`}>
                <div className="chunk-header">
                  <span
                    className={`fidelity-badge fidelity-badge--${fidelity}`}
                    title={config.label}
                  >
                    {config.icon} {config.label}
                  </span>
                  {chunk.speaker && (
                    <span className="chunk-speaker">{chunk.speaker}</span>
                  )}
                  <span className="chunk-time">
                    {formatTime(chunk.start_offset)}&ndash;
                    {formatTime(chunk.end_offset)}
                  </span>
                </div>
                <blockquote className="chunk-content">{chunk.content}</blockquote>
                <div className="chunk-source">{chunk.source_file}</div>
              </div>
            );
          })}
      </section>

      {selected.labels && Object.keys(selected.labels).length > 0 && (
        <section className="evidence-labels">
          <h4>Labels</h4>
          <ul>
            {Object.entries(selected.labels)
              .filter(([k]) => !k.startsWith("_"))
              .map(([k, v]) => (
                <li key={k}>
                  <code>{k}</code>: {v}
                </li>
              ))}
          </ul>
        </section>
      )}
    </div>
  );
}
