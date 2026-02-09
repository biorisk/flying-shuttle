import { useCallback, useRef, useState } from "react";
import { uploads } from "../services/api";
import type { Upload } from "../types/model";

const ACCEPTED = ".mp3,.wav,.m4a,.ogg,.flac,.webm";

interface Props {
  onUploaded?: (u: Upload) => void;
}

export default function AudioUpload({ onUploaded }: Props) {
  const [dragging, setDragging] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const handleFile = useCallback(
    async (file: File) => {
      setError(null);
      setUploading(true);
      try {
        const u = await uploads.create(file);
        onUploaded?.(u);
      } catch (e: unknown) {
        setError(e instanceof Error ? e.message : "Upload failed");
      } finally {
        setUploading(false);
      }
    },
    [onUploaded],
  );

  const onDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      setDragging(false);
      const file = e.dataTransfer.files[0];
      if (file) handleFile(file);
    },
    [handleFile],
  );

  const onDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    setDragging(true);
  }, []);

  const onDragLeave = useCallback(() => setDragging(false), []);

  const onSelect = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      const file = e.target.files?.[0];
      if (file) handleFile(file);
      e.target.value = "";
    },
    [handleFile],
  );

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
        onChange={onSelect}
        hidden
      />
      {uploading ? (
        <span className="upload-status">Uploading...</span>
      ) : (
        <span className="upload-prompt">
          Drop audio file or <u>browse</u>
        </span>
      )}
      {error && <span className="upload-error">{error}</span>}
    </div>
  );
}
