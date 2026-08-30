import { useCallback, useRef, useState } from "react";
import { uploads } from "../services/api";
import type { Upload } from "../types/model";

const ACCEPTED = ".mp3,.wav,.m4a,.ogg,.flac,.webm,.txt,.md,.markdown,.text";

interface Props {
  onUploaded?: (u: Upload) => void;
}

export default function AudioUpload({ onUploaded }: Props) {
  const [dragging, setDragging] = useState(false);
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(
    null,
  );
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const handleFiles = useCallback(
    async (files: File[]) => {
      if (files.length === 0) return;
      setError(null);
      setProgress({ done: 0, total: files.length });

      // Upload every file in parallel; only persist to disk here (defer),
      // then kick off processing once the whole batch has landed.
      const results = await Promise.allSettled(
        files.map(async (file) => {
          const u = await uploads.create(file, { defer: true });
          onUploaded?.(u);
          setProgress((p) => (p ? { ...p, done: p.done + 1 } : p));
          return u;
        }),
      );

      const uploaded = results.flatMap((r) =>
        r.status === "fulfilled" ? [r.value] : [],
      );
      const failed = results.length - uploaded.length;

      if (uploaded.length > 0) {
        try {
          await uploads.process(uploaded.map((u) => u.id));
        } catch (e: unknown) {
          setError(
            e instanceof Error ? e.message : "Failed to start processing",
          );
        }
      }

      if (failed > 0) {
        setError(
          `${failed} of ${results.length} file${results.length > 1 ? "s" : ""} failed to upload`,
        );
      }
      setProgress(null);
    },
    [onUploaded],
  );

  const onDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      setDragging(false);
      handleFiles(Array.from(e.dataTransfer.files));
    },
    [handleFiles],
  );

  const onDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    setDragging(true);
  }, []);

  const onDragLeave = useCallback(() => setDragging(false), []);

  const onSelect = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      handleFiles(Array.from(e.target.files ?? []));
      e.target.value = "";
    },
    [handleFiles],
  );

  const uploading = progress !== null;

  return (
    <div
      className={`audio-upload-zone${dragging ? " dragging" : ""}`}
      onDrop={onDrop}
      onDragOver={onDragOver}
      onDragLeave={onDragLeave}
      onClick={() => inputRef.current?.click()}
    >
      <input
        ref={inputRef}
        type="file"
        accept={ACCEPTED}
        multiple
        onChange={onSelect}
        hidden
      />
      {uploading ? (
        <span className="upload-status">
          Uploading {progress.done}/{progress.total}...
        </span>
      ) : (
        <span className="upload-prompt">
          Drop audio or transcript files, or <u>browse</u>
        </span>
      )}
      {error && <span className="upload-error">{error}</span>}
    </div>
  );
}
