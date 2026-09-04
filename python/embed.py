import csv
import sys
import time
import argparse
import struct
from pathlib import Path

# Increase the CSV field size limit to handle large text chunks
csv.field_size_limit(sys.maxsize)

# 1. Configuration
MODEL_PATH = "mlx-community/embeddinggemma-300m-bf16"
BATCH_SIZE = 1  # Crucial for 8GB RAM
CHUNK_SIZE = 300
OVERLAP = 150

# Model and tokenizer are loaded lazily — only when embedding is required.
# The --convert path never loads the model.
_model = None
_tokenizer = None


# EmbeddingGemma document-side task prefix (see config_sentence_transformers).
DOC_PREFIX = "title: none | text: "


def _load_model():
    global _model, _tokenizer
    if _model is None:
        from mlx_embeddings import load
        print(f"--- Loading {MODEL_PATH} ---")
        _model, _tokenizer = load(MODEL_PATH)
    return _model, _tokenizer


def get_embeddings(text_list):
    import numpy as np
    model, tokenizer = _load_model()

    enc = tokenizer._tokenizer(
        [DOC_PREFIX + t for t in text_list],
        padding=True, truncation=True, max_length=2048, return_tensors="mlx",
    )
    out = model(enc["input_ids"], attention_mask=enc["attention_mask"])
    v = np.array(out.text_embeds, dtype=np.float32)
    norm = np.linalg.norm(v, axis=-1, keepdims=True)
    norm[norm == 0] = 1.0
    return v / norm


# ---------------------------------------------------------------------------
# Binary .fembed format
# ---------------------------------------------------------------------------

FEMB_MAGIC = b'FEMB'
FEMB_VERSION = 1


def write_fembed(out_path, records):
    """
    Write records to a .fembed binary file.

    records: list of (source_file: str, start_token: int, text: str, embedding: array-like of float)

    File format:
      Header (16 bytes):
        [4]  magic "FEMB"
        [2]  version uint16 LE = 1
        [2]  dims uint16 LE
        [4]  record_count uint32 LE
        [4]  reserved 0x00*4
      Per record:
        [4]  source_file_len uint32 LE
        [N]  source_file UTF-8
        [4]  start_token int32 LE
        [4]  text_len uint32 LE
        [M]  text UTF-8
        [dims*4]  embedding float32 LE array
    """
    if not records:
        print("No records to write.")
        return

    dims = len(records[0][3])
    count = len(records)

    with open(out_path, 'wb') as f:
        # Header
        f.write(FEMB_MAGIC)
        f.write(struct.pack('<H', FEMB_VERSION))
        f.write(struct.pack('<H', dims))
        f.write(struct.pack('<I', count))
        f.write(b'\x00' * 4)  # reserved

        # Records
        for source_file, start_token, text, embedding in records:
            sf_bytes = source_file.encode('utf-8')
            text_bytes = text.encode('utf-8')
            f.write(struct.pack('<I', len(sf_bytes)))
            f.write(sf_bytes)
            f.write(struct.pack('<i', start_token))
            f.write(struct.pack('<I', len(text_bytes)))
            f.write(text_bytes)
            f.write(struct.pack(f'<{dims}f', *embedding))


# ---------------------------------------------------------------------------
# Convert: .embed TSV → .fembed (no model required)
# ---------------------------------------------------------------------------

EMBED_HEADER = ["file_name", "start_token", "embedding", "text"]


def read_embed_tsv(embed_path):
    """
    Read a .embed TSV file and return a list of records suitable for write_fembed.

    Returns: list of (source_file, start_token, text, embedding_floats)
    Raises: ValueError on bad header or unparseable rows.
    """
    records = []
    dims = None

    with open(embed_path, 'r', newline='', encoding='utf-8') as f:
        reader = csv.reader(f, delimiter='\t', strict=False)

        try:
            header = next(reader)
        except StopIteration:
            raise ValueError(f"{embed_path}: file is empty")

        if header != EMBED_HEADER:
            raise ValueError(
                f"{embed_path}: unexpected header {header!r}, expected {EMBED_HEADER!r}"
            )

        for lineno, row in enumerate(reader, start=2):
            if not row:
                continue  # skip blank lines

            if len(row) < 4:
                print(f"  Warning: line {lineno} has {len(row)} columns, skipping.")
                continue

            source_file = row[0]
            start_token = int(row[1])
            text = row[3]

            embedding = [float(x) for x in row[2].split(',')]

            if dims is None:
                dims = len(embedding)
            elif len(embedding) != dims:
                print(
                    f"  Warning: line {lineno} has {len(embedding)} dims, expected {dims}. Skipping."
                )
                continue

            records.append((source_file, start_token, text, embedding))

    return records


def convert_embed_file(embed_path, out_path=None):
    """
    Convert a single .embed TSV file to .fembed binary format.
    out_path defaults to embed_path with .fembed extension.
    """
    embed_path = Path(embed_path)
    if out_path is None:
        out_path = embed_path.with_suffix('.fembed')
    out_path = Path(out_path)

    print(f"Converting {embed_path} → {out_path} ...")
    records = read_embed_tsv(embed_path)
    if not records:
        print(f"  No records found in {embed_path}, skipping.")
        return 0

    dims = len(records[0][3])
    write_fembed(out_path, records)
    print(f"  Written {len(records)} records ({dims} dims) to {out_path}")
    return len(records)


def convert_embed_dir(embed_dir, out_dir=None):
    """
    Convert all *.embed files in embed_dir to .fembed.
    out_dir defaults to embed_dir (output files alongside inputs).
    """
    embed_dir = Path(embed_dir)
    if out_dir is None:
        out_dir = embed_dir
    out_dir = Path(out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    embed_files = sorted(embed_dir.glob("*.embed"))
    if not embed_files:
        print(f"No .embed files found in {embed_dir}")
        return

    total_records = 0
    for embed_file in embed_files:
        out_path = out_dir / embed_file.with_suffix('.fembed').name
        total_records += convert_embed_file(embed_file, out_path)

    print(f"\nDone. Converted {len(embed_files)} file(s), {total_records} total records.")
    print(f"Next step: POST /api/v1/ingest/directory with path={out_dir}")


# ---------------------------------------------------------------------------
# Embedding pipeline (requires model)
# ---------------------------------------------------------------------------

def process_directory(text_dir, out_file, fmt='binary'):
    """
    Processes all .txt files in a directory, chunks them, generates embeddings,
    and saves to an output file.

    fmt='binary': .fembed binary format (default, no resume).
    fmt='tsv': legacy .embed TSV format with resume support.
    """
    print(f"--- Processing directory {text_dir} (format={fmt}) ---")
    start_time = time.time()

    text_dir_path = Path(text_dir)
    out_file_path = Path(out_file)

    all_txt_files = list(text_dir_path.glob("*.txt"))
    if not all_txt_files:
        print(f"No .txt files found in {text_dir}")
        return

    if fmt == 'binary':
        _process_directory_binary(text_dir_path, out_file_path, all_txt_files)
    else:
        _process_directory_tsv(text_dir_path, out_file_path, all_txt_files)

    end_time = time.time()
    print(f"--- Finished processing ---")
    print(f"Embeddings saved to {out_file}")
    print(f"Total time taken: {end_time - start_time:.2f} seconds")


def _process_directory_binary(text_dir_path, out_file_path, all_txt_files):
    """Write .fembed binary output (no resume — always fresh)."""
    _, tokenizer = _load_model()
    total_files = len(all_txt_files)
    all_records = []

    for file_index, txt_file in enumerate(all_txt_files):
        print(f"Processing file {file_index + 1} of {total_files}: {txt_file.name}...")
        text = txt_file.read_text(encoding='utf-8')

        tokens = tokenizer._tokenizer.encode(text)
        chunks_to_process = []

        for i in range(0, len(tokens), CHUNK_SIZE - OVERLAP):
            chunk_tokens = tokens[i:i + CHUNK_SIZE]
            if not chunk_tokens:
                continue
            chunk_text = tokenizer._tokenizer.decode(chunk_tokens, skip_special_tokens=True)
            chunk_text = chunk_text.replace("\n", " ").replace("\t", " ")
            chunks_to_process.append({
                "file_name": txt_file.name,
                "start_token": i,
                "text": chunk_text,
            })

        if not chunks_to_process:
            print(f"  No chunks to process for {txt_file.name}.")
            continue

        print(f"  Found {len(chunks_to_process)} chunks to process.")

        for i in range(0, len(chunks_to_process), BATCH_SIZE):
            batch = chunks_to_process[i:i + BATCH_SIZE]
            batch_texts = [c['text'] for c in batch]
            embeddings = get_embeddings(batch_texts)
            for j, chunk in enumerate(batch):
                all_records.append((
                    chunk['file_name'],
                    chunk['start_token'],
                    chunk['text'],
                    embeddings[j],
                ))

        print(f"  Completed {txt_file.name}")

    print(f"Writing {len(all_records)} records to {out_file_path} ...")
    write_fembed(out_file_path, all_records)


def _process_directory_tsv(text_dir_path, out_file_path, all_txt_files):
    """Write TSV output (.embed) with resume support."""
    _, tokenizer = _load_model()
    processed_chunks = {}
    is_resuming = False
    if out_file_path.exists() and out_file_path.stat().st_size > 0:
        is_resuming = True
        with open(out_file_path, 'r', newline='', encoding='utf-8') as f:
            reader = csv.reader(f, delimiter='\t')
            try:
                header = next(reader)
                if header != EMBED_HEADER:
                    print("Warning: Output file has an invalid header. Starting over.")
                    is_resuming = False
                else:
                    for row in reader:
                        if row:
                            file_name, start_token = row[0], int(row[1])
                            processed_chunks.setdefault(file_name, set()).add(start_token)
            except StopIteration:
                is_resuming = False

        if is_resuming and processed_chunks:
            print(f"Resuming. Found chunks for {len(processed_chunks)} files in {out_file_path}.")

    total_files = len(all_txt_files)
    open_mode = 'a' if is_resuming else 'w'
    write_header = not is_resuming

    with open(out_file_path, open_mode, newline='', encoding='utf-8') as f:
        writer = csv.writer(f, delimiter='\t')
        if write_header:
            writer.writerow(EMBED_HEADER)

        for file_index, txt_file in enumerate(all_txt_files):
            print(f"Processing file {file_index + 1} of {total_files}: {txt_file.name}...")
            text = txt_file.read_text(encoding='utf-8')
            tokens = tokenizer._tokenizer.encode(text)

            file_chunks_to_process = []
            existing_chunks = processed_chunks.get(txt_file.name, set())

            for i in range(0, len(tokens), CHUNK_SIZE - OVERLAP):
                if i in existing_chunks:
                    continue
                chunk_tokens = tokens[i:i + CHUNK_SIZE]
                if not chunk_tokens:
                    continue
                chunk_text = tokenizer._tokenizer.decode(chunk_tokens, skip_special_tokens=True)
                chunk_text = chunk_text.replace("\n", " ").replace("\t", " ")
                file_chunks_to_process.append({
                    "file_name": txt_file.name,
                    "start_token": i,
                    "text": chunk_text,
                })

            if not file_chunks_to_process:
                print(f"  No new chunks to process for {txt_file.name}.")
                continue

            print(f"  Found {len(file_chunks_to_process)} new chunks to process.")

            for i in range(0, len(file_chunks_to_process), BATCH_SIZE):
                batch_chunks = file_chunks_to_process[i:i + BATCH_SIZE]
                batch_texts = [chunk['text'] for chunk in batch_chunks]
                embeddings = get_embeddings(batch_texts)
                for j, chunk in enumerate(batch_chunks):
                    embedding_str = ",".join(map(str, embeddings[j]))
                    writer.writerow([
                        chunk['file_name'],
                        chunk['start_token'],
                        embedding_str,
                        chunk['text'],
                    ])
            print(f"  Completed embedding new chunks for {txt_file.name}")


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------

if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="Generate or convert embeddings for Flying Shuttle.",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Generate embeddings from text files (binary output, default):
  python embed.py --text-dir ./transcripts/

  # Convert existing .embed TSV → .fembed (no model load):
  python embed.py --convert --embed-file recordings.embed
  python embed.py --convert --embed-dir ./embeddings/ --out-dir ./converted/

  # Then ingest into running server:
  curl -X POST http://localhost:8080/api/v1/ingest/directory \\
       -H 'Content-Type: application/json' \\
       -d '{"path": "/absolute/path/to/converted"}'
""",
    )

    # Embedding mode
    parser.add_argument("--text-dir", type=str, help="Directory of .txt files to embed.")
    parser.add_argument("--out-file", type=str, help="Output file path.")
    parser.add_argument("--format", type=str, choices=["tsv", "binary"], default="binary",
                        help="Output format for --text-dir mode. Default: binary.")

    # Convert mode
    parser.add_argument("--convert", action="store_true",
                        help="Convert .embed TSV file(s) to .fembed binary. Does not load the model.")
    parser.add_argument("--embed-file", type=str, help="Single .embed file to convert.")
    parser.add_argument("--embed-dir", type=str, help="Directory of *.embed files to convert.")
    parser.add_argument("--out-dir", type=str,
                        help="Output directory for --embed-dir conversions (default: same as --embed-dir).")

    args = parser.parse_args()

    if args.convert:
        # ── Convert mode: no model needed ──────────────────────────────────
        if not args.embed_file and not args.embed_dir:
            parser.error("--convert requires --embed-file or --embed-dir")

        if args.embed_file:
            out = Path(args.out_file) if args.out_file else None
            convert_embed_file(args.embed_file, out)

        if args.embed_dir:
            convert_embed_dir(args.embed_dir, args.out_dir)

    elif args.text_dir:
        # ── Embedding mode: loads model ─────────────────────────────────────
        out_file = args.out_file
        if not out_file:
            base = Path(args.text_dir).name
            out_file = f"{base}.fembed" if args.format == "binary" else f"{base}.embed"
        process_directory(args.text_dir, out_file, fmt=args.format)

    else:
        # ── Smoke-test: embed two sentences ────────────────────────────────
        print("No mode specified. Running smoke test (loads model)...")
        test_chunks = [
            "The board meeting started at 9 AM with a focus on expansion.",
            "Revenue projections for 2026 show a 20% increase in the tech sector.",
        ]
        start = time.time()
        vectors = get_embeddings(test_chunks)
        end = time.time()
        print(f"Success! Generated {vectors.shape[0]} vectors of dim {vectors.shape[1]}.")
        print(f"Time taken: {end - start:.2f} seconds")
