//go:build integration

package store

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
)

func mustCreateNodeForOps(t *testing.T, s *Store) *Node {
	t.Helper()

	ctx := context.Background()

	u, err := s.CreateUser(ctx, "friend@example.com", "hash", false)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	n, err := s.CreateNode(ctx, NewNodeParams{
		UserID:       u.ID,
		Name:         "departure",
		GameURL:      "https://www.airlinemanager.com/",
		GameUsername: "player1",
	})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}

	return n
}

func TestEnqueueAndClaimOperation(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	n := mustCreateNodeForOps(t, s)

	op, err := s.EnqueueOperation(ctx, n.ID, OpReconcile, nil)
	if err != nil {
		t.Fatalf("EnqueueOperation() error = %v", err)
	}
	if op.Status != StatusPending {
		t.Fatalf("EnqueueOperation() status = %q, want %q", op.Status, StatusPending)
	}

	claimed, err := s.ClaimPendingOperations(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimPendingOperations() error = %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("ClaimPendingOperations() returned %d ops, want 1", len(claimed))
	}
	if claimed[0].Status != StatusRunning {
		t.Fatalf("claimed operation status = %q, want %q", claimed[0].Status, StatusRunning)
	}

	// a second claim must not see the same (now-running) row again
	claimedAgain, err := s.ClaimPendingOperations(ctx, 10)
	if err != nil {
		t.Fatalf("second ClaimPendingOperations() error = %v", err)
	}
	if len(claimedAgain) != 0 {
		t.Fatalf("second ClaimPendingOperations() returned %d ops, want 0 (already running)", len(claimedAgain))
	}

	if err := s.FinishOperation(ctx, op.ID, true, nil); err != nil {
		t.Fatalf("FinishOperation() error = %v", err)
	}
}

func TestFinishOperationRecordsFailure(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	n := mustCreateNodeForOps(t, s)

	op, err := s.EnqueueOperation(ctx, n.ID, OpReconcile, nil)
	if err != nil {
		t.Fatalf("EnqueueOperation() error = %v", err)
	}

	if _, err := s.ClaimPendingOperations(ctx, 10); err != nil {
		t.Fatalf("ClaimPendingOperations() error = %v", err)
	}

	errMsg := "ansible-playbook exited 1"
	if err := s.FinishOperation(ctx, op.ID, false, &errMsg); err != nil {
		t.Fatalf("FinishOperation() error = %v", err)
	}

	var stored NodeOperation
	if err := s.db.Get(&stored, `SELECT id, node_id, op_type, payload, status, error_message, created_at, updated_at FROM node_operations WHERE id = $1`, op.ID); err != nil {
		t.Fatalf("re-reading operation: %v", err)
	}

	if stored.Status != StatusFailed {
		t.Fatalf("stored status = %q, want %q", stored.Status, StatusFailed)
	}
	if stored.ErrorMessage == nil || *stored.ErrorMessage != errMsg {
		t.Fatalf("stored error_message = %v, want %q", stored.ErrorMessage, errMsg)
	}
}

func TestNodeOperationSurvivesNodeDeletion(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	n := mustCreateNodeForOps(t, s)

	op, err := s.EnqueueOperation(ctx, n.ID, OpDelete, []byte(`{"container_name":"ambot-departure"}`))
	if err != nil {
		t.Fatalf("EnqueueOperation() error = %v", err)
	}

	// n isn't the default node, so a direct delete (bypassing the HTTP
	// layer's own enqueue-then-delete flow, deliberately, to isolate what
	// this test is checking) is enough here.
	if _, err := s.db.Exec(`DELETE FROM nodes WHERE id = $1`, n.ID); err != nil {
		t.Fatalf("deleting node: %v", err)
	}

	claimed, err := s.ClaimPendingOperations(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimPendingOperations() error = %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("ClaimPendingOperations() returned %d ops after node deletion, want 1 (payload-only op must survive)", len(claimed))
	}
	if claimed[0].ID != op.ID {
		t.Fatalf("claimed operation id = %d, want %d", claimed[0].ID, op.ID)
	}
	if claimed[0].NodeID != nil {
		t.Fatalf("claimed operation NodeID = %v, want nil (ON DELETE SET NULL)", claimed[0].NodeID)
	}
	// compare parsed, not raw bytes: JSONB round-trips through Postgres'
	// own canonical text form (e.g. adds a space after ":"), which is a
	// storage-format detail, not a change to the payload's actual content.
	var got, want map[string]any
	if err := json.Unmarshal(claimed[0].Payload, &got); err != nil {
		t.Fatalf("unmarshaling claimed payload: %v", err)
	}
	if err := json.Unmarshal([]byte(`{"container_name":"ambot-departure"}`), &want); err != nil {
		t.Fatalf("unmarshaling expected payload: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("claimed operation Payload = %v, want %v", got, want)
	}
}

func TestClaimPendingOperationsIsConcurrencySafe(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	n := mustCreateNodeForOps(t, s)

	const totalOps = 20
	for range totalOps {
		if _, err := s.EnqueueOperation(ctx, n.ID, OpReconcile, nil); err != nil {
			t.Fatalf("EnqueueOperation() error = %v", err)
		}
	}

	// Two "orchestrators" racing to claim work concurrently must never
	// see the same row (FOR UPDATE SKIP LOCKED) and must together claim
	// every row exactly once.
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		claimed = map[int64]int{} // op id -> number of times it was claimed
	)

	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ops, err := s.ClaimPendingOperations(ctx, totalOps)
			if err != nil {
				t.Errorf("ClaimPendingOperations() error = %v", err)
				return
			}

			mu.Lock()
			defer mu.Unlock()
			for _, op := range ops {
				claimed[op.ID]++
			}
		}()
	}
	wg.Wait()

	if len(claimed) != totalOps {
		t.Fatalf("claimed %d distinct operations, want %d", len(claimed), totalOps)
	}
	for id, count := range claimed {
		if count != 1 {
			t.Fatalf("operation %d claimed %d times, want exactly 1", id, count)
		}
	}
}
