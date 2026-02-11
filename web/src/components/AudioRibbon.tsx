import { useCallback, useEffect, useRef, useState } from "react";
import { useDraggable } from "@dnd-kit/core";
import { chunks as chunksApi } from "../services/api";
import type { Chunk } from "../types/model";

// Simple hash-based color assignment for clusters.
const CLUSTER_COLORS = [
  "rgba(100, 108, 255, 0.6)",
  "rgba(76, 175, 80, 0.6)",
  "rgba(255, 152, 0, 0.6)",
  "rgba(233, 30, 99, 0.6)",
  "rgba(0, 188, 212, 0.6)",
  "rgba(156, 39, 176, 0.6)",
  "rgba(255, 87, 34, 0.6)",
  "rgba(63, 81, 181, 0.6)",
];

function clusterColor(index: number): string {
  return CLUSTER_COLORS[index % CLUSTER_COLORS.length];
}

// Assign a pseudo-cluster based on content similarity (simple word overlap).
function assignCluster(chunk: Chunk, centroids: string[]): number {
  const words = new Set(chunk.content.toLowerCase().split(/\s+/));
  let bestIdx = 0;
  let bestOverlap = 0;

  for (let i = 0; i < centroids.length; i++) {
    const centroidWords = centroids[i].toLowerCase().split(/\s+/);
    let overlap = 0;
    for (const w of centroidWords) {
      if (words.has(w)) overlap++;
    }
    if (overlap > bestOverlap) {
      bestOverlap = overlap;
      bestIdx = i;
    }
  }
  return bestIdx;
}

// Build simple cluster centroids from chunks by grouping every N chunks.
function buildCentroids(chunks: Chunk[], numClusters: number): string[] {
  const groupSize = Math.max(1, Math.ceil(chunks.length / numClusters));
  const centroids: string[] = [];
  for (let i = 0; i < numClusters && i * groupSize < chunks.length; i++) {
    const group = chunks.slice(i * groupSize, (i + 1) * groupSize);
    centroids.push(group.map((c) => c.content).join(" "));
  }
  return centroids;
}

interface RibbonSegment {
  chunk: Chunk;
  clusterIdx: number;
  color: string;
}

/** Individually draggable ribbon segment. */
function DraggableSegment({
  seg,
  selected,
  onClick,
}: {
  seg: RibbonSegment;
  index: number;
  selected: boolean;
  onClick: () => void;
}) {
  const elRef = useRef<HTMLDivElement>(null);

  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
    id: `ribbon-${seg.chunk.id}`,
    data: { type: "chunk" as const, chunk: seg.chunk, color: seg.color },
  });

  const ref = useCallback(
    (el: HTMLDivElement | null) => {
      setNodeRef(el);
      (elRef as React.MutableRefObject<HTMLDivElement | null>).current = el;
    },
    [setNodeRef],
  );

  return (
    <div
      ref={ref}
      className={`ribbon-segment ${selected ? "selected" : ""} ${isDragging ? "dragging" : ""}`}
      style={{
        backgroundColor: seg.color,
        flex: Math.max(1, seg.chunk.content.length / 20),
      }}
      onClick={onClick}
      title={`${seg.chunk.source_file} [${seg.chunk.start_offset}-${seg.chunk.end_offset}] — drag to outline`}
      {...attributes}
      {...listeners}
    />
  );
}

export default function AudioRibbon() {
  const [chunks, setChunks] = useState<Chunk[]>([]);
  const [segments, setSegments] = useState<RibbonSegment[]>([]);
  const [selectedIdx, setSelectedIdx] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    chunksApi
      .list()
      .then((allChunks) => {
        // Sort by source file then offset for sequential display.
        const sorted = allChunks.sort((a, b) => {
          if (a.source_file !== b.source_file) return a.source_file.localeCompare(b.source_file);
          return a.start_offset - b.start_offset;
        });
        setChunks(sorted);

        // Build clusters.
        const numClusters = Math.min(8, Math.max(2, Math.ceil(sorted.length / 3)));
        const centroids = buildCentroids(sorted, numClusters);
        const segs = sorted.map((chunk) => {
          const clusterIdx = assignCluster(chunk, centroids);
          return { chunk, clusterIdx, color: clusterColor(clusterIdx) };
        });
        setSegments(segs);
        setLoading(false);
      })
      .catch(() => setLoading(false));
  }, []);

  const handleSegmentClick = useCallback((idx: number) => {
    setSelectedIdx(idx);
  }, []);

  if (loading) return <p className="pane-placeholder">Loading ribbon...</p>;
  if (chunks.length === 0) return null;

  return (
    <div className="audio-ribbon">
      <h4 className="section-heading">Audio Ribbon</h4>
      <div className="ribbon-container">
        <div className="ribbon-bar">
          {segments.map((seg, i) => (
            <DraggableSegment
              key={seg.chunk.id}
              seg={seg}
              index={i}
              selected={selectedIdx === i}
              onClick={() => handleSegmentClick(i)}
            />
          ))}
        </div>
        <div className="ribbon-transcript">
          {selectedIdx !== null && segments[selectedIdx] && (
            <div className="ribbon-selected-chunk">
              <div className="ribbon-chunk-meta">
                <span
                  className="ribbon-cluster-dot"
                  style={{ backgroundColor: segments[selectedIdx].color }}
                />
                <span className="ribbon-chunk-source">
                  {segments[selectedIdx].chunk.source_file}
                </span>
                {segments[selectedIdx].chunk.speaker && (
                  <span className="ribbon-chunk-speaker">
                    {segments[selectedIdx].chunk.speaker}
                  </span>
                )}
              </div>
              <p className="ribbon-chunk-text">
                {segments[selectedIdx].chunk.content}
              </p>
            </div>
          )}
          {selectedIdx === null && (
            <p className="ribbon-hint">Click a ribbon segment to view its content</p>
          )}
        </div>
      </div>
    </div>
  );
}
