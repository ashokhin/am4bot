//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/ashokhin/am4bot/internal/auth"
	"github.com/ashokhin/am4bot/internal/store"
)

// mustCreateUser is mustCreateAdmin's non-admin counterpart, for tests
// that only need a logged-in regular user, not the admin endpoints.
func mustCreateUser(t *testing.T, st *store.Store, email, password string) *store.User {
	t.Helper()

	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	u, err := st.CreateUser(context.Background(), email, hash, false)
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	return u
}

func TestCreateNodeEnqueuesReconcile(t *testing.T) {
	srv, _, st, _ := testServerWithDeps(t)
	ctx := context.Background()

	mustCreateUser(t, st, "user@example.com", "userpass123")

	c := &client{base: srv.URL, jar: map[string]string{}}
	resp, _ := c.do(t, "POST", "/api/auth/login", loginRequest{Login: "user@example.com", Password: "userpass123"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}

	// drain the default node's reconcile op, if any (there shouldn't be
	// one -- see handleCreateUser's comment -- but keep this test focused
	// on the node created below regardless).
	if _, err := st.ClaimPendingOperations(ctx, 100); err != nil {
		t.Fatalf("draining pending operations: %v", err)
	}

	resp, body := c.do(t, "POST", "/api/nodes", createNodeRequest{
		Name: "departure", GameUsername: "player1", GamePassword: "gamepass1",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("creating node status = %d, want 201", resp.StatusCode)
	}
	nodeID := int64(body["id"].(float64))

	ops, err := st.ClaimPendingOperations(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimPendingOperations() error = %v", err)
	}

	found := false
	for _, op := range ops {
		if op.OpType == store.OpReconcile && op.NodeID != nil && *op.NodeID == nodeID {
			found = true
		}
	}
	if !found {
		t.Fatalf("no reconcile operation enqueued for node %d; claimed: %+v", nodeID, ops)
	}
}

func TestDeleteNodeEnqueuesDeleteWithSnapshot(t *testing.T) {
	srv, _, st, _ := testServerWithDeps(t)
	ctx := context.Background()

	u := mustCreateUser(t, st, "user@example.com", "userpass123")

	n, err := st.CreateNode(ctx, store.NewNodeParams{
		UserID: u.ID, Name: "departure", GameURL: "https://www.airlinemanager.com/", GameUsername: "player1",
	})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}

	// Simulate the node already having been provisioned once, the way
	// EnsureNodeProvisioned would leave it -- otherwise handleDeleteNode
	// has nothing to snapshot and correctly enqueues nothing.
	containerName, _, err := st.EnsureNodeProvisioned(ctx, n.ID, "test-host", 9200, 9299)
	if err != nil {
		t.Fatalf("EnsureNodeProvisioned() error = %v", err)
	}

	// drain whatever's pending (the default node's setup, this node's own
	// creation) so only the delete op below is left to assert on.
	if _, err := st.ClaimPendingOperations(ctx, 100); err != nil {
		t.Fatalf("draining pending operations: %v", err)
	}

	c := &client{base: srv.URL, jar: map[string]string{}}
	resp, _ := c.do(t, "POST", "/api/auth/login", loginRequest{Login: "user@example.com", Password: "userpass123"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}

	resp, _ = c.do(t, "DELETE", "/api/nodes/"+strconv.FormatInt(n.ID, 10), nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", resp.StatusCode)
	}

	ops, err := st.ClaimPendingOperations(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimPendingOperations() error = %v", err)
	}

	if len(ops) != 1 {
		t.Fatalf("claimed %d operations, want 1 (the delete)", len(ops))
	}
	if ops[0].OpType != store.OpDelete {
		t.Fatalf("OpType = %q, want %q", ops[0].OpType, store.OpDelete)
	}
	if ops[0].NodeID != nil {
		t.Fatalf("NodeID = %v, want nil (node was deleted, ON DELETE SET NULL)", ops[0].NodeID)
	}

	var payload store.DeleteOperationPayload
	if err := json.Unmarshal(ops[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshaling payload: %v", err)
	}
	if payload.ContainerName != containerName {
		t.Fatalf("payload.ContainerName = %q, want %q", payload.ContainerName, containerName)
	}
	if payload.TargetHost == nil || *payload.TargetHost != "test-host" {
		t.Fatalf("payload.TargetHost = %v, want test-host", payload.TargetHost)
	}
}
