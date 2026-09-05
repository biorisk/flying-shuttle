-- node_chunks (the pre-evidence whole-chunk association) is fully superseded
-- by the evidence table and has no readers left in the code. Migration 005
-- already carried any rows over as full-span evidence. Drop it.
--
-- Old snapshots that still serialize node_chunks restore fine: the restore
-- path converts model.NodeChunkAssoc entries straight into evidence rows.
DROP TABLE IF EXISTS node_chunks;
