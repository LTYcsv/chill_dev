package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"time"

	_ "github.com/lib/pq"
)

const migrationSQL = `
CREATE TABLE IF NOT EXISTS tt_checkpoints (
    id         BIGSERIAL PRIMARY KEY,
    project_id TEXT NOT NULL,
    env        TEXT NOT NULL,
    seq        BIGINT NOT NULL,
    graph_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS tt_diffs (
    id         BIGSERIAL PRIMARY KEY,
    project_id TEXT NOT NULL,
    env        TEXT NOT NULL,
    seq        BIGINT NOT NULL,
    source     TEXT NOT NULL,
    patch_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_tt_diffs_lookup ON tt_diffs (project_id, env, seq);
CREATE INDEX IF NOT EXISTS idx_tt_checkpoints_lookup ON tt_checkpoints (project_id, env, created_at);
`

type TTStore struct {
	db *sql.DB
}

type checkpointRow struct {
	Seq       int64
	GraphJSON []byte
}

func newTTStore(databaseURL string) (*TTStore, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	if _, err := db.Exec(migrationSQL); err != nil {
		return nil, err
	}
	return &TTStore{db: db}, nil
}

func (t *TTStore) saveCheckpoint(projectID, env string, seq int64, g InfraGraph) {
	data, err := json.Marshal(g)
	if err != nil {
		log.Printf("[graph] checkpoint marshal: %v", err)
		return
	}
	if _, err := t.db.Exec(
		`INSERT INTO tt_checkpoints (project_id, env, seq, graph_json) VALUES ($1,$2,$3,$4)`,
		projectID, env, seq, data,
	); err != nil {
		log.Printf("[graph] checkpoint save: %v", err)
	}
}

func (t *TTStore) saveDiff(projectID, env string, seq int64, source string, diff GraphDiff) {
	data, _ := json.Marshal(diff)
	if _, err := t.db.Exec(
		`INSERT INTO tt_diffs (project_id, env, seq, source, patch_json) VALUES ($1,$2,$3,$4,$5)`,
		projectID, env, seq, source, data,
	); err != nil {
		log.Printf("[graph] diff save: %v", err)
	}
}

func (t *TTStore) getCheckpointBefore(projectID, env string, at time.Time) (*checkpointRow, error) {
	var cp checkpointRow
	err := t.db.QueryRow(
		`SELECT seq, graph_json FROM tt_checkpoints
		 WHERE project_id=$1 AND env=$2 AND created_at <= $3
		 ORDER BY created_at DESC LIMIT 1`,
		projectID, env, at,
	).Scan(&cp.Seq, &cp.GraphJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &cp, nil
}

func (t *TTStore) getDiffsAfter(projectID, env string, afterSeq int64, at time.Time) ([]DiffRecord, error) {
	rows, err := t.db.Query(
		`SELECT id, seq, source, patch_json, created_at FROM tt_diffs
		 WHERE project_id=$1 AND env=$2 AND seq > $3 AND created_at <= $4
		 ORDER BY seq ASC`,
		projectID, env, afterSeq, at,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDiffRows(rows, projectID, env)
}

func (t *TTStore) listDiffs(projectID, env string, limit int) ([]DiffRecord, error) {
	rows, err := t.db.Query(
		`SELECT id, seq, source, patch_json, created_at FROM tt_diffs
		 WHERE project_id=$1 AND env=$2
		 ORDER BY seq DESC LIMIT $3`,
		projectID, env, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDiffRows(rows, projectID, env)
}

func scanDiffRows(rows *sql.Rows, projectID, env string) ([]DiffRecord, error) {
	var out []DiffRecord
	for rows.Next() {
		var r DiffRecord
		var patchJSON []byte
		if err := rows.Scan(&r.ID, &r.Seq, &r.Source, &patchJSON, &r.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(patchJSON, &r.Diff); err != nil {
			return nil, err
		}
		r.ProjectID = projectID
		r.Env = env
		out = append(out, r)
	}
	return out, rows.Err()
}

func (t *TTStore) maxSeqPerCombo() map[string]int64 {
	out := make(map[string]int64)
	rows, err := t.db.Query(`SELECT project_id, env, MAX(seq) FROM tt_diffs GROUP BY project_id, env`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var projectID, env string
		var maxSeq int64
		if rows.Scan(&projectID, &env, &maxSeq) == nil {
			out[projectID+":"+env] = maxSeq
		}
	}
	return out
}
