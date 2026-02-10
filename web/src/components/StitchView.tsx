import { useCallback, useEffect, useMemo } from "react";
import { useStitchStore } from "../stores/stitchStore";
import { useThreadStore } from "../stores/threadStore";
import { exporting } from "../services/api";

function glueLabel(level: number): string {
  if (level <= 0) return "Raw";
  if (level <= 25) return "Minimal";
  if (level <= 50) return "Light";
  if (level <= 75) return "Smooth";
  return "Full";
}

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
  }, [fetchStitch, viewMode, threadId]);

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

  // Immediate visual feedback: scale glue span opacity with glueLevel.
  const glueOpacity = useMemo(() => {
    if (glueLevel <= 0) return 0;
    // Map 1-100 to 0.15-1.0 opacity range.
    return 0.15 + (glueLevel / 100) * 0.85;
  }, [glueLevel]);

  const label = glueLabel(glueLevel);

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
          <span className="stitch-glue-tag" data-level={label.toLowerCase()}>
            {label}
          </span>
        </label>
        <button
          className="stitch-refresh-btn"
          onClick={handleRefresh}
          title="Refresh preview"
        >
          &#x21bb;
        </button>
        <a
          className="stitch-export-btn"
          href={exporting.downloadUrl(
            threadId ?? undefined,
            threadId ? threads.find((t) => t.id === threadId)?.name : "Manuscript",
          )}
          download
          title="Export as Markdown"
        >
          &#x2913; .md
        </a>
      </div>

      {error && <p className="stitch-error">{error}</p>}

      {result && (
        <>
          <div className="stitch-stats">
            <span>{result.nodes.length} nodes</span>
            <span className="stitch-stat-sep">|</span>
            <span>{result.stitch.stats.total_chars} chars</span>
            <span className="stitch-stat-sep">|</span>
            <span>
              {Math.round(result.stitch.stats.glue_ratio * 100)}% glue
            </span>
            {loading && <span className="stitch-refreshing">refreshing...</span>}
          </div>
          <div className={`stitch-content ${loading ? "stitch-content--stale" : ""}`}>
            {result.stitch.spans.map((span, i) =>
              span.type === "chunk" ? (
                <span
                  key={i}
                  className="stitch-span-chunk"
                  title={`Chunk: ${span.chunk_id ?? "unknown"}`}
                >
                  {span.text}
                </span>
              ) : (
                <span
                  key={i}
                  className="stitch-span-glue"
                  style={{ opacity: glueOpacity }}
                  title="AI-generated transition"
                >
                  {span.text}
                </span>
              ),
            )}
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

      {!result && loading && <p className="stitch-loading">Stitching...</p>}
    </div>
  );
}
