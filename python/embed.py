import mlx.core as mx
from mlx_lm import load
import numpy as np
import time
import argparse
import os
import struct
import csv
import sys
from pathlib import Path

# Increase the CSV field size limit to handle large text chunks
csv.field_size_limit(sys.maxsize)

# 1. Configuration
MODEL_PATH = "./Qwen3-Embedding-4B-4bit-DWQ"
BATCH_SIZE = 1  # Crucial for 8GB RAM
CHUNK_SIZE = 300
OVERLAP = 150

print(f"--- Loading {MODEL_PATH} ---")
# load() handles the 4-bit weights automatically on Apple Silicon
model, tokenizer = load(MODEL_PATH)

def get_embeddings(text_list):
    # Instructions help Qwen3-Embedding categorize the context
    instruction = "Represent this transcript for retrieval: "
    inputs = [instruction + t for t in text_list]

    # Tokenize with padding
    tokens = tokenizer._tokenizer(inputs, padding=True, return_tensors="np")
    input_ids = mx.array(tokens['input_ids'])

    # Generate hidden states
    # No grad needed for embedding inference
    output = model.model(input_ids)

    # Qwen3-Embedding uses the last token's hidden state for its vector
    # Shape: [batch, sequence_length, hidden_dim] -> [batch, hidden_dim]
    embeddings = output[:, -1, :]

    # Normalize for Cosine Similarity (Vector DB standard)
    norm = mx.linalg.norm(embeddings, axis=-1, keepdims=True)
    normalized = embeddings / norm

    return np.array(normalized.astype(mx.float32))


# --- Binary .fembed format ---

FEMB_MAGIC = b'FEMB'
FEMB_VERSION = 1

def write_fembed(out_path, records):
    """
    Write records to a .fembed binary file.

    records: list of (source_file: str, start_token: int, text: str, embedding: np.ndarray)

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
            f.write(struct.pack(f'<{dims}f', *embedding.tolist()))


def process_directory(text_dir, out_file, fmt='binary'):
    """
    Processes all .txt files in a directory, chunks them, generates embeddings,
    and saves them to an output file.

    fmt='tsv': legacy TSV format (.embed), supports resume.
    fmt='binary': binary .fembed format, always writes fresh.
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
    """Write .fembed binary output (no resume support — always fresh)."""
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
    processed_chunks = {}
    is_resuming = False
    if out_file_path.exists() and out_file_path.stat().st_size > 0:
        is_resuming = True
        with open(out_file_path, 'r', newline='', encoding='utf-8') as f:
            reader = csv.reader(f, delimiter='\t')
            try:
                header = next(reader)
                if header != ["file_name", "start_token", "embedding", "text"]:
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
            writer.writerow(["file_name", "start_token", "embedding", "text"])

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


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Generate embeddings for text files.")
    parser.add_argument("--text-dir", type=str, help="Directory containing .txt files to process.")
    parser.add_argument("--out-file", type=str, help="Output file for embeddings.")
    parser.add_argument("--format", type=str, choices=["tsv", "binary"], default="binary",
                        help="Output format: 'binary' (.fembed) or 'tsv' (.embed). Default: binary.")

    args = parser.parse_args()

    if args.text_dir:
        out_file = args.out_file
        if not out_file:
            base = Path(args.text_dir).name
            out_file = f"{base}.fembed" if args.format == "binary" else f"{base}.embed"

        process_directory(args.text_dir, out_file, fmt=args.format)
    else:
        # Original example code
        print("No --text-dir provided. Running example...")
        test_chunks = [
            "The board meeting started at 9 AM with a focus on expansion.",
            "Revenue projections for 2026 show a 20% increase in the tech sector."
        ]

        start = time.time()
        vectors = get_embeddings(test_chunks)
        end = time.time()

        print(f"Success! Generated {vectors.shape[0]} vectors.")
        if vectors.shape[0] > 0:
            print(f"Vector Dimensions: {vectors.shape[1]}")
        print(f"Time taken: {end - start:.2f} seconds")
