# Automated Transcript Summarization & Mind-Map Indexing Plan

## Executive Summary
This document outlines an optimized, lightweight batch-processing architecture designed to extract hierarchical mind-map structures from video/audio transcripts and build a semantic search index.

The entire workflow is specifically tailored for an **Apple Silicon M1 Mac with 8 GB of Unified RAM**, prioritizing minimal memory footprint, high inference throughput, and zero background resource consumption.

---

## Hardware & System Constraints

- **System:** Apple M1 Mac Air / Pro (8 GB Unified Memory)
- **Available Unified RAM Target:** ~3.0 GB – 4.0 GB (leaving ~4.0 GB+ for macOS and display server)
- **Primary Goal:** Eliminate swap memory usage (`mach_vm`), prevent thermal throttling during batch operations, and ensure 100% offline processing.

---

## Recommended Model Stack

### 1. Structural Extraction & Summarization Model
* **Model:** `Qwen3-4B-Instruct`
* **Format:** MLX Native (`mlx-community/Qwen3-4B-Instruct-2507-6bit` or `4bit`)
* **Footprint:** ~2.2 GB (4-bit) to ~3.2 GB (6-bit)
* **Role:** Analyzes transcript text, extracts core themes, and outputs a hierarchical Markdown outline representing mind-map nodes.

### 2. Semantic Embedding Model
* **Model:** `google/embeddinggemma-300m` (or `snowflake-arctic-embed-m-v2.0`)
* **Footprint:** ~300 MB
* **Role:** Vectorizes extracted mind-map node strings into dense numerical embeddings for fast similarity search.

---

## Architectural Strategy: Two-Phase Decoupled Pipeline

To run comfortably within 8 GB RAM without system swapping, the pipeline operates in **two strictly separated, sequential phases**. The generative LLM and the embedding model **never coexist in RAM**.

```
┌─────────────────────────────────────────────────────────┐
│              PHASE 1: BATCH EXTRACTION                  │
│                                                         │
│  Transcripts ──>  [Qwen3-4B via MLX]  ──>  Node Outline │
│                         │                               │
│                   (Purge RAM)                           │
└─────────────────────────┼───────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│              PHASE 2: VECTOR INDEXING                   │
│                                                         │
│  Node Outline ──> [Gemma-300M Embedder] ──>  LanceDB    │
└─────────────────────────────────────────────────────────┘
```

---

## Pipeline Execution Workflow

### Phase 1: Batch Node Extraction (MLX Framework)
1. **Engine Selection:** Uses Apple's native **MLX framework (`mlx-lm`)**, which provides zero-copy memory management and direct Metal GPU acceleration on Apple Silicon.
2. **Sequential Batch Processing:** Iterates over raw transcript files sequentially.
3. **Prompt Design:** Instructs `Qwen3-4B` to produce a structured **Markdown visual outline** (`# Main Subject`, `## Topic`, `- Subtopic: Summary`). This bypasses fragile JSON parsing overhead while maintaining clear parent-child node relationships.
4. **Memory Hygiene:** Synchronizes the Metal execution stream (`mx.eval()`) and triggers Python garbage collection (`gc.collect()`) after processing each transcript to prevent memory leak accumulation across batch runs.
5. **Full Memory Purge:** Destroys the model instance and frees all allocated Unified RAM prior to starting Phase 2.

### Phase 2: Embedding & Local Vector Storage
1. **Embedding Initialization:** Loads the compact ~300 MB embedding model into memory.
2. **Node Parsing:** Reads the generated Markdown outlines, splitting them into discrete structural nodes (headers, bullet points, and summaries).
3. **Vector Generation:** Converts each visual node into a vector representation.
4. **Database Storage:** Saves vectors, raw text nodes, and source document metadata into **LanceDB**—an embedded, zero-server vector database that runs directly off disk files with no persistent background process.

---

## Key Benefits of This Architecture

- **Peak Efficiency on M1:** Leverages Apple-native Metal APIs (MLX) for up to 2x faster token generation compared to standard GGUF engines.
- **Zero Memory Bloat:** Decoupling Phase 1 and Phase 2 guarantees peak memory consumption stays under 3.5 GB at all times.
- **No Background Services:** Bypasses server daemons like Ollama; processing runs purely in-process and terminates cleanly.
- **Semantic Search Capability:** Enables instant vector search across all mind-map nodes with full trace-back to the original transcript file.
