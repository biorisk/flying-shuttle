import { useCallback, useRef, useState } from "react";
import { useDraggable } from "@dnd-kit/core";
import { chunks as chunksApi, search as searchApi } from "../services/api";
import type { Chunk } from "../types/model";

interface Hit {
  chunk: Chunk;
  score: number;
}

const HIT_COLOR = "rgba(100, 108, 255, 0.6)";

/** A search hit that can be dragged onto the outline (same payload as ribbon segments). */
function DraggableHit({ chunk }: { chunk: Chunk }) {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
    id: `search-${chunk.id}`,
    data: { type: "chunk" as const, chunk, color: HIT_COLOR },
  });
  const preview =
    chunk.content.length > 200 ? chunk.content.slice(0, 200) + "…" : chunk.content;

  return (
    <div
      ref={setNodeRef}
      className={`search-hit ${isDragging ? "dragging" : ""}`}
      title="Drag onto a bullet to attach as evidence"
      {...attributes}
      {...listeners}
    >
      <div className="search-hit-source">{chunk.source_file}</div>
      <p className="search-hit-text">{preview}</p>
    </div>
  );
}

export default function SearchBox() {
  const [q, setQ] = useState("");
  const [hits, setHits] = useState<Hit[]>([]);
  const [loading, setLoading] = useState(false);
  const [searched, setSearched] = useState(false);
  const seq = useRef(0);

  const run = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      const query = q.trim();
      if (!query) {
        setHits([]);
        setSearched(false);
        return;
      }
      const mine = ++seq.current;
      setLoading(true);
      try {
        const results = await searchApi.query(query, 15);
        const resolved = await Promise.all(
          results.map((r) => chunksApi.get(r.chunk_id).catch(() => null)),
        );
        if (mine !== seq.current) return; // superseded by a newer search
        const merged: Hit[] = results
          .map((r, i) =>
            resolved[i] ? { chunk: resolved[i] as Chunk, score: r.score } : null,
          )
          .filter((h): h is Hit => h !== null);
        setHits(merged);
        setSearched(true);
      } catch {
        if (mine === seq.current) {
          setHits([]);
          setSearched(true);
        }
      } finally {
        if (mine === seq.current) setLoading(false);
      }
    },
    [q],
  );

  const clear = () => {
    setQ("");
    setHits([]);
    setSearched(false);
  };

  return (
    <div className="search-box">
      <form className="search-form" onSubmit={run}>
        <input
          className="search-input"
          type="search"
          placeholder="Search chunks…"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
        <button className="search-submit" type="submit" disabled={loading}>
          {loading ? "…" : "Go"}
        </button>
      </form>

      {searched && !loading && (
        <div className="search-meta">
          <span>
            {hits.length} {hits.length === 1 ? "result" : "results"}
          </span>
          <button className="search-clear" onClick={clear}>
            clear
          </button>
        </div>
      )}

      {hits.length > 0 && (
        <div className="search-results">
          {hits.map((h) => (
            <DraggableHit key={h.chunk.id} chunk={h.chunk} />
          ))}
        </div>
      )}

      {searched && !loading && hits.length === 0 && (
        <p className="search-empty">No matching chunks.</p>
      )}
    </div>
  );
}
