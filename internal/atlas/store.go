package atlas

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/biorisk/flying-shuttle/internal/ingest"
	"github.com/google/uuid"
)

// Store is the Atlas persistence surface. It is deliberately separate from
// doc.Store: the Atlas is a self-contained, disposable subsystem and does not
// belong in the main domain interface.
type Store interface {
	// CreateBuild inserts a new build in "building" status and fills b.ID /
	// b.CreatedAt / b.Status.
	CreateBuild(b *Build) error
	// SetBuildStatus updates a build's status, chunk count, and error text.
	SetBuildStatus(id string, status BuildStatus, chunkCount int, errMsg string) error
	// InsertRegions bulk-inserts regions and their members for a build, in one
	// transaction. Region ids are generated when empty.
	InsertRegions(buildID string, regions []Region) error
	// InsertLinks bulk-inserts region links for a build, in one transaction.
	InsertLinks(buildID string, links []Link) error
	// InsertTranscriptDigests bulk-inserts one digest per source file for a
	// build, in one transaction (Phase D).
	InsertTranscriptDigests(buildID string, digests []TranscriptDigest) error
	// ChunkLabelsMissing returns the subset of ids that still need the LLM:
	// those with no row, or only a best-effort "head" fallback row (so a
	// later build retries them once the LLM is back). Order is unspecified.
	ChunkLabelsMissing(ids []string) ([]string, error)
	// GetChunkLabels returns chunk_id -> label for the given ids (only those
	// with a row).
	GetChunkLabels(ids []string) (map[string]string, error)
	// PutChunkLabels upserts chunk labels in one transaction.
	PutChunkLabels(labels []ChunkLabel) error
	// GetDigests returns the content-addressed digest cache rows for the
	// given input hashes (only those present).
	GetDigests(hashes []string) (map[string]CachedDigest, error)
	// PutDigest upserts one digest cache row by input_hash.
	PutDigest(d CachedDigest) error
	// SetDigestVec stores the digest embedding for one cache row.
	SetDigestVec(inputHash string, vec []float32) error
	// SetRegionDigest updates a region's digest fields (Phase B).
	SetRegionDigest(regionID string, d Digest) error
	// SetRegionDigestVec stores a region's digest embedding (Phase C).
	SetRegionDigestVec(regionID string, vec []float32) error
	// SetMemberKeywords updates one membership's extractive keyword tags.
	SetMemberKeywords(regionID, chunkID string, kw []string) error
	// GetBuild returns a build with its regions (incl. members) and links.
	GetBuild(id string) (*Build, error)
	// CurrentBuild returns the newest "ready" build, or ErrNoBuild.
	CurrentBuild() (*Build, error)
	// ListBuilds returns build headers (no regions/links), newest first.
	ListBuilds() ([]Build, error)
	// PruneExcept deletes every build except keepID (cascades to regions,
	// links, and memberships).
	PruneExcept(keepID string) error
	// DeleteBuild removes one build (cascades). Missing id is not an error.
	DeleteBuild(id string) error
}

// sqlStore implements Store against the shared *sql.DB. Obtain the handle from
// doc.SQLiteStore.DB().
type sqlStore struct{ db *sql.DB }

// NewStore returns a SQLite-backed Atlas store over an existing connection.
func NewStore(db *sql.DB) Store { return &sqlStore{db: db} }

const rfc3339ms = "2006-01-02T15:04:05.000Z07:00"

func (s *sqlStore) CreateBuild(b *Build) error {
	if b.ID == "" {
		b.ID = uuid.NewString()
	}
	if b.Status == "" {
		b.Status = StatusBuilding
	}
	b.CreatedAt = time.Now().UTC()
	params := b.Params
	if params == "" {
		params = "{}"
	}
	_, err := s.db.Exec(
		`INSERT INTO atlas_build (id, created_at, status, chunk_count, params_json, error)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		b.ID, b.CreatedAt.Format(rfc3339ms), string(b.Status), b.ChunkCount, params, b.Error,
	)
	return err
}

func (s *sqlStore) SetBuildStatus(id string, status BuildStatus, chunkCount int, errMsg string) error {
	res, err := s.db.Exec(
		`UPDATE atlas_build SET status = ?, chunk_count = ?, error = ? WHERE id = ?`,
		string(status), chunkCount, errMsg, id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("atlas: build %s not found", id)
	}
	return nil
}

func (s *sqlStore) InsertRegions(buildID string, regions []Region) (err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	for i := range regions {
		r := &regions[i]
		if r.ID == "" {
			r.ID = uuid.NewString()
		}
		r.BuildID = buildID
		var digestVec any
		if len(r.DigestVec) > 0 {
			digestVec = ingest.Float32sToBytes(r.DigestVec)
		}
		if _, err = tx.Exec(
			`INSERT INTO atlas_region
			   (id, build_id, centroid_vec, chunk_count,
			    digest_title, digest_abstract, digest_keywords, digest_source, digest_vec)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, buildID, ingest.Float32sToBytes(r.Centroid), r.ChunkCount,
			r.Digest.Title, r.Digest.Abstract, joinKeywords(r.Digest.Keywords), r.Digest.Source, digestVec,
		); err != nil {
			return err
		}
		for _, m := range r.Members {
			if _, err = tx.Exec(
				`INSERT INTO atlas_region_chunk (region_id, chunk_id, distance, keywords)
				 VALUES (?, ?, ?, ?)`,
				r.ID, m.ChunkID, m.Distance, joinKeywords(m.Keywords),
			); err != nil {
				return err
			}
		}
	}
	err = tx.Commit()
	return err
}

func (s *sqlStore) InsertLinks(buildID string, links []Link) (err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	for _, l := range links {
		a, b := l.RegionA, l.RegionB
		if a > b {
			a, b = b, a
		}
		if a == b {
			continue
		}
		if _, err = tx.Exec(
			`INSERT INTO atlas_region_link (build_id, region_a_id, region_b_id, weight)
			 VALUES (?, ?, ?, ?)`,
			buildID, a, b, l.Weight,
		); err != nil {
			return err
		}
	}
	err = tx.Commit()
	return err
}

func (s *sqlStore) InsertTranscriptDigests(buildID string, digests []TranscriptDigest) (err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	for _, td := range digests {
		if td.SourceFile == "" {
			continue
		}
		if _, err = tx.Exec(
			`INSERT INTO atlas_transcript
			   (build_id, source_file, chunk_count,
			    digest_title, digest_abstract, digest_keywords, digest_source)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			buildID, td.SourceFile, td.ChunkCount,
			td.Digest.Title, td.Digest.Abstract, joinKeywords(td.Digest.Keywords), td.Digest.Source,
		); err != nil {
			return err
		}
	}
	err = tx.Commit()
	return err
}

func (s *sqlStore) ChunkLabelsMissing(ids []string) ([]string, error) {
	// A chunk still needs the LLM if it has no row at all, OR only a
	// best-effort "head" fallback row (LLM was down, or mangled that line).
	// A later build re-attempts those; a real "llm:<model>" row is final.
	done, err := s.chunkLabelsWhere(ids, "AND source LIKE 'llm:%'")
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, id := range ids {
		if _, ok := done[id]; !ok {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

// GetChunkLabels returns chunk_id -> label for the given ids that have any row
// (LLM or head fallback) — the drill-down view shows whatever's there.
func (s *sqlStore) GetChunkLabels(ids []string) (map[string]string, error) {
	return s.chunkLabelsWhere(ids, "")
}

func (s *sqlStore) chunkLabelsWhere(ids []string, extra string) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	// Chunk the IN list to stay well under SQLite's variable limit.
	const batch = 400
	for start := 0; start < len(ids); start += batch {
		end := start + batch
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		q := `SELECT chunk_id, label FROM atlas_chunk_label WHERE chunk_id IN (?` +
			strings.Repeat(",?", len(chunk)-1) + `) ` + extra
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := s.db.Query(q, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, label string
			if err := rows.Scan(&id, &label); err != nil {
				rows.Close()
				return nil, err
			}
			out[id] = label
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

func (s *sqlStore) PutChunkLabels(labels []ChunkLabel) (err error) {
	if len(labels) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	for _, l := range labels {
		if l.ChunkID == "" {
			continue
		}
		if _, err = tx.Exec(
			`INSERT INTO atlas_chunk_label (chunk_id, label, source) VALUES (?, ?, ?)
			 ON CONFLICT(chunk_id) DO UPDATE SET label = excluded.label, source = excluded.source`,
			l.ChunkID, l.Label, l.Source,
		); err != nil {
			return err
		}
	}
	err = tx.Commit()
	return err
}

func (s *sqlStore) GetDigests(hashes []string) (map[string]CachedDigest, error) {
	out := make(map[string]CachedDigest, len(hashes))
	if len(hashes) == 0 {
		return out, nil
	}
	const batch = 400
	for start := 0; start < len(hashes); start += batch {
		end := start + batch
		if end > len(hashes) {
			end = len(hashes)
		}
		chunk := hashes[start:end]
		q := `SELECT input_hash, kind, title, abstract, keywords, vec, source
		        FROM atlas_digest WHERE input_hash IN (?` + strings.Repeat(",?", len(chunk)-1) + `)`
		args := make([]any, len(chunk))
		for i, h := range chunk {
			args[i] = h
		}
		rows, err := s.db.Query(q, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c CachedDigest
			var kw string
			var vec []byte
			if err := rows.Scan(&c.InputHash, &c.Kind, &c.Digest.Title, &c.Digest.Abstract, &kw, &vec, &c.Source); err != nil {
				rows.Close()
				return nil, err
			}
			c.Digest.Keywords = splitKeywords(kw)
			c.Digest.Source = c.Source
			if len(vec) > 0 {
				c.Vec = ingest.BytesToFloat32s(vec)
			}
			out[c.InputHash] = c
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

func (s *sqlStore) PutDigest(d CachedDigest) error {
	var vec any
	if len(d.Vec) > 0 {
		vec = ingest.Float32sToBytes(d.Vec)
	}
	_, err := s.db.Exec(
		`INSERT INTO atlas_digest (input_hash, kind, title, abstract, keywords, vec, source)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(input_hash) DO UPDATE SET
		   kind = excluded.kind, title = excluded.title, abstract = excluded.abstract,
		   keywords = excluded.keywords, vec = excluded.vec, source = excluded.source`,
		d.InputHash, d.Kind, d.Digest.Title, d.Digest.Abstract, joinKeywords(d.Digest.Keywords), vec, d.Source,
	)
	return err
}

func (s *sqlStore) SetDigestVec(inputHash string, vec []float32) error {
	_, err := s.db.Exec(
		`UPDATE atlas_digest SET vec = ? WHERE input_hash = ?`,
		ingest.Float32sToBytes(vec), inputHash,
	)
	return err
}

func (s *sqlStore) SetRegionDigest(regionID string, d Digest) error {
	_, err := s.db.Exec(
		`UPDATE atlas_region
		    SET digest_title = ?, digest_abstract = ?, digest_keywords = ?, digest_source = ?
		  WHERE id = ?`,
		d.Title, d.Abstract, joinKeywords(d.Keywords), d.Source, regionID,
	)
	return err
}

func (s *sqlStore) SetRegionDigestVec(regionID string, vec []float32) error {
	_, err := s.db.Exec(
		`UPDATE atlas_region SET digest_vec = ? WHERE id = ?`,
		ingest.Float32sToBytes(vec), regionID,
	)
	return err
}

func (s *sqlStore) SetMemberKeywords(regionID, chunkID string, kw []string) error {
	_, err := s.db.Exec(
		`UPDATE atlas_region_chunk SET keywords = ? WHERE region_id = ? AND chunk_id = ?`,
		joinKeywords(kw), regionID, chunkID,
	)
	return err
}

func (s *sqlStore) GetBuild(id string) (*Build, error) {
	b, err := s.scanBuildHeader(s.db.QueryRow(
		`SELECT id, created_at, status, chunk_count, params_json, error
		   FROM atlas_build WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoBuild
	}
	if err != nil {
		return nil, err
	}
	if err := s.loadBuildBody(b); err != nil {
		return nil, err
	}
	return b, nil
}

func (s *sqlStore) CurrentBuild() (*Build, error) {
	b, err := s.scanBuildHeader(s.db.QueryRow(
		`SELECT id, created_at, status, chunk_count, params_json, error
		   FROM atlas_build WHERE status = ? ORDER BY created_at DESC LIMIT 1`,
		string(StatusReady)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoBuild
	}
	if err != nil {
		return nil, err
	}
	if err := s.loadBuildBody(b); err != nil {
		return nil, err
	}
	return b, nil
}

func (s *sqlStore) ListBuilds() ([]Build, error) {
	rows, err := s.db.Query(
		`SELECT id, created_at, status, chunk_count, params_json, error
		   FROM atlas_build ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Build
	for rows.Next() {
		b, err := s.scanBuildHeader(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

func (s *sqlStore) PruneExcept(keepID string) error {
	_, err := s.db.Exec(`DELETE FROM atlas_build WHERE id <> ?`, keepID)
	return err
}

func (s *sqlStore) DeleteBuild(id string) error {
	_, err := s.db.Exec(`DELETE FROM atlas_build WHERE id = ?`, id)
	return err
}

// --- scan helpers ---

type rowScanner interface{ Scan(dest ...any) error }

func (s *sqlStore) scanBuildHeader(row rowScanner) (*Build, error) {
	var b Build
	var created, status string
	if err := row.Scan(&b.ID, &created, &status, &b.ChunkCount, &b.Params, &b.Error); err != nil {
		return nil, err
	}
	b.CreatedAt, _ = time.Parse(rfc3339ms, created)
	b.Status = BuildStatus(status)
	return &b, nil
}

func (s *sqlStore) loadBuildBody(b *Build) error {
	regRows, err := s.db.Query(
		`SELECT id, centroid_vec, chunk_count,
		        digest_title, digest_abstract, digest_keywords, digest_source, digest_vec
		   FROM atlas_region WHERE build_id = ? ORDER BY chunk_count DESC, id`, b.ID)
	if err != nil {
		return err
	}
	defer regRows.Close()

	byID := map[string]*Region{}
	for regRows.Next() {
		var r Region
		var centroid, digestVec []byte
		var kw string
		if err := regRows.Scan(
			&r.ID, &centroid, &r.ChunkCount,
			&r.Digest.Title, &r.Digest.Abstract, &kw, &r.Digest.Source, &digestVec,
		); err != nil {
			return err
		}
		r.BuildID = b.ID
		r.Centroid = ingest.BytesToFloat32s(centroid)
		r.Digest.Keywords = splitKeywords(kw)
		if len(digestVec) > 0 {
			r.DigestVec = ingest.BytesToFloat32s(digestVec)
		}
		b.Regions = append(b.Regions, r)
	}
	if err := regRows.Err(); err != nil {
		return err
	}
	for i := range b.Regions {
		byID[b.Regions[i].ID] = &b.Regions[i]
	}

	memRows, err := s.db.Query(
		`SELECT rc.region_id, rc.chunk_id, rc.distance, rc.keywords
		   FROM atlas_region_chunk rc
		   JOIN atlas_region r ON r.id = rc.region_id
		  WHERE r.build_id = ?
		  ORDER BY rc.distance`, b.ID)
	if err != nil {
		return err
	}
	defer memRows.Close()
	for memRows.Next() {
		var regionID string
		var m Member
		var kw string
		if err := memRows.Scan(&regionID, &m.ChunkID, &m.Distance, &kw); err != nil {
			return err
		}
		m.Keywords = splitKeywords(kw)
		if r := byID[regionID]; r != nil {
			r.Members = append(r.Members, m)
		}
	}
	if err := memRows.Err(); err != nil {
		return err
	}

	linkRows, err := s.db.Query(
		`SELECT region_a_id, region_b_id, weight
		   FROM atlas_region_link WHERE build_id = ? ORDER BY weight DESC`, b.ID)
	if err != nil {
		return err
	}
	defer linkRows.Close()
	for linkRows.Next() {
		var l Link
		if err := linkRows.Scan(&l.RegionA, &l.RegionB, &l.Weight); err != nil {
			return err
		}
		b.Links = append(b.Links, l)
	}
	if err := linkRows.Err(); err != nil {
		return err
	}

	tRows, err := s.db.Query(
		`SELECT source_file, chunk_count, digest_title, digest_abstract, digest_keywords, digest_source
		   FROM atlas_transcript WHERE build_id = ? ORDER BY source_file`, b.ID)
	if err != nil {
		return err
	}
	defer tRows.Close()
	for tRows.Next() {
		var td TranscriptDigest
		var kw string
		if err := tRows.Scan(
			&td.SourceFile, &td.ChunkCount,
			&td.Digest.Title, &td.Digest.Abstract, &kw, &td.Digest.Source,
		); err != nil {
			return err
		}
		td.Digest.Keywords = splitKeywords(kw)
		b.Transcripts = append(b.Transcripts, td)
	}
	return tRows.Err()
}
