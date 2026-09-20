package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// NodeOperation is one queued unit of work for the orchestrator: "make
// this node's containers match its current row" (OpReconcile) or "tear
// down what payload describes, the node row may already be gone"
// (OpDelete). apiserver only ever inserts these; only the orchestrator
// reads and updates them -- see migrations/0001_init.sql's
// design notes for why the two never call each other directly.
type NodeOperation struct {
	ID           int64           `db:"id"`
	NodeID       *int64          `db:"node_id"`
	OpType       string          `db:"op_type"`
	Payload      json.RawMessage `db:"payload"`
	Status       string          `db:"status"`
	ErrorMessage *string         `db:"error_message"`
	CreatedAt    time.Time       `db:"created_at"`
	UpdatedAt    time.Time       `db:"updated_at"`
}

const (
	OpReconcile = "reconcile"
	OpDelete    = "delete"
)

const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
)

// DeleteOperationPayload is NodeOperation.Payload's shape for OpDelete --
// a snapshot of what the orchestrator needs to tear down a node's
// containers, taken by apiserver's handleDeleteNode before the node row
// itself is deleted (see EnqueueOperation's doc comment on why node_id
// alone can't be relied on afterward).
type DeleteOperationPayload struct {
	ContainerName    string  `json:"container_name"`
	VPNContainerName string  `json:"vpn_container_name"`
	TargetHost       *string `json:"target_host"`
}

// EnqueueOperation inserts a new pending operation. payload should be
// nil for OpReconcile (the orchestrator re-reads the node row itself,
// which still exists) and a snapshot of whatever teardown needs for
// OpDelete (the node row is gone by the time this runs).
func (s *Store) EnqueueOperation(ctx context.Context, nodeID int64, opType string, payload json.RawMessage) (*NodeOperation, error) {
	if payload == nil {
		payload = json.RawMessage(`{}`)
	}

	var op NodeOperation

	err := s.db.GetContext(ctx, &op, `
		INSERT INTO node_operations (node_id, op_type, payload)
		VALUES ($1, $2, $3)
		RETURNING id, node_id, op_type, payload, status, error_message, created_at, updated_at
	`, nodeID, opType, payload)
	if err != nil {
		return nil, fmt.Errorf("enqueueing %s operation for node %d: %w", opType, nodeID, err)
	}

	return &op, nil
}

// ClaimPendingOperations atomically marks up to limit pending operations
// as running and returns them, oldest first. Uses
// "FOR UPDATE SKIP LOCKED" so it's safe to run more than one orchestrator
// process concurrently -- each claims a disjoint set of rows instead of
// blocking on or double-processing the same one.
func (s *Store) ClaimPendingOperations(ctx context.Context, limit int) ([]NodeOperation, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op if Commit already ran

	var ops []NodeOperation

	if err := tx.SelectContext(ctx, &ops, `
		SELECT id, node_id, op_type, payload, status, error_message, created_at, updated_at
		FROM node_operations
		WHERE status = $1
		ORDER BY created_at
		LIMIT $2
		FOR UPDATE SKIP LOCKED
	`, StatusPending, limit); err != nil {
		return nil, fmt.Errorf("selecting pending operations: %w", err)
	}

	if len(ops) > 0 {
		ids := make([]int64, len(ops))
		for i, op := range ops {
			ids[i] = op.ID
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE node_operations SET status = $1, updated_at = now() WHERE id = ANY($2)`,
			StatusRunning, ids,
		); err != nil {
			return nil, fmt.Errorf("marking operations running: %w", err)
		}

		for i := range ops {
			ops[i].Status = StatusRunning
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing claim: %w", err)
	}

	return ops, nil
}

// FinishOperation records the outcome of a claimed operation. errMsg
// should be nil on success; on failure it's a short, safe-to-store
// description -- never raw command output, which could contain the
// decrypted secrets the orchestrator handled transiently while running
// this operation.
func (s *Store) FinishOperation(ctx context.Context, id int64, succeeded bool, errMsg *string) error {
	status := StatusSucceeded
	if !succeeded {
		status = StatusFailed
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE node_operations SET status = $2, error_message = $3, updated_at = now() WHERE id = $1`,
		id, status, errMsg,
	)
	if err != nil {
		return fmt.Errorf("finishing operation %d: %w", id, err)
	}

	return checkRowsAffected(res, "node operation")
}
