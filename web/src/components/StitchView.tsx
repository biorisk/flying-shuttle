import { useCallback, useEffect } from "react";
import { useStitchStore } from "../stores/stitchStore";
import { useThreadStore } from "../stores/threadStore";

export default function StitchView() {
  const {
    result,
    viewMode,
    threadId,
    glueLevel,
    loading,
    error,
    setThreadId,
    setGlueLevel,
    fetchStitch,
  } = useStitchStore();
  const { threads, fetchThreads } = useThreadStore();

  useEffect(() => {
    fetchThreads();
  }, [fetchThreads]);

  useEffect(() => {
    fetchStitch();
  }, [fetchStitch, viewMode, threadId, glueLevel]);

  const handleThreadChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      const val = e.target.value;
      setThreadId(val || null);
    },
    [setThreadId],
  );

  const handleGlueChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      setGlueLevel(parseInt(e.target.value, 10));
    },
    [setGlueLevel],
  );

  const handleRefresh = useCallback(() => {
    fetchStitch();
  }, [fetchStitch]);

  return (
    <div className="stitch-view">
      <div className="stitch-toolbar">
        <select
          className="stitch-thread-select"
          value={threadId ?? ""}
          onChange={handleThreadChange}
        >
          <option value="">Full Manuscript</option>
          {threads.map((t) => (
            <option key={t.id} value={t.id}>
              {t.name}
            </option>
          ))}
        </select>
        <label className="stitch-glue-label">
          Glue:
          <input
            type="range"
            className="stitch-glue-slider"
            min={0}
            max={100}
            value={glueLevel}
            onChange={handleGlueChange}
          />
          <span className="stitch-glue-value">{glueLevel}%</span>
        </label>
        <button
          className="stitch-refresh-btn"
          onClick={handleRefresh}
          title="Refresh preview"
        >
          &#x21bb;
        </button>
      </div>

      {loading && <p className="stitch-loading">Stitching...</p>}
      {error && <p className="stitch-error">{error}</p>}

      {result && !loading && (
        <>
          <div className="stitch-stats">
            <span>{result.nodes.length} nodes</span>
            <span className="stitch-stat-sep">|</span>
            <span>{result.stitch.stats.total_chars} chars</span>
            <span className="stitch-stat-sep">|</span>
            <span>
              {Math.round(result.stitch.stats.glue_ratio * 100)}% glue
            </span>
          </div>
          <div className="stitch-content">
            {result.stitch.spans.map((span, i) => (
              <span
                key={i}
                className={
                  span.type === "chunk" ? "stitch-span-chunk" : "stitch-span-glue"
                }
                title={
                  span.type === "chunk"
                    ? `Chunk: ${span.chunk_id ?? "unknown"}`
                    : "AI-generated transition"
                }
              >
                {span.text}
              </span>
            ))}
            {result.stitch.spans.length === 0 && (
              <p className="stitch-empty">
                No content to stitch.{" "}
                {viewMode === "thread"
                  ? "Add nodes to the selected thread."
                  : "Add nodes to the outline."}
              </p>
            )}
          </div>
        </>
      )}
    </div>
  );
}
