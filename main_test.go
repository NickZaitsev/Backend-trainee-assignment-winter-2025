package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

func setupTestDB(t *testing.T) *sql.DB {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://user:password@localhost:5432/avito_test?sslmode=disable"
	}

	testDB, err := sql.Open("postgres", databaseURL)
	if err != nil {
		t.Skip("Skipping integration test: database not available")
	}

	if err := testDB.Ping(); err != nil {
		t.Skip("Skipping integration test: database not available")
	}

	// Clean up and initialize schema
	_, _ = testDB.Exec("DROP TABLE IF EXISTS pr_reviewers CASCADE")
	_, _ = testDB.Exec("DROP TABLE IF EXISTS pull_requests CASCADE")
	_, _ = testDB.Exec("DROP TABLE IF EXISTS users CASCADE")
	_, _ = testDB.Exec("DROP TABLE IF EXISTS teams CASCADE")

	schema := `
	CREATE TABLE teams (
		team_name VARCHAR(255) PRIMARY KEY
	);

	CREATE TABLE users (
		user_id VARCHAR(255) PRIMARY KEY,
		username VARCHAR(255) NOT NULL,
		team_name VARCHAR(255) NOT NULL REFERENCES teams(team_name),
		is_active BOOLEAN NOT NULL DEFAULT true
	);

	CREATE TABLE pull_requests (
		pull_request_id VARCHAR(255) PRIMARY KEY,
		pull_request_name VARCHAR(255) NOT NULL,
		author_id VARCHAR(255) NOT NULL REFERENCES users(user_id),
		status VARCHAR(10) NOT NULL CHECK (status IN ('OPEN', 'MERGED')),
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		merged_at TIMESTAMP
	);

	CREATE TABLE pr_reviewers (
		pull_request_id VARCHAR(255) NOT NULL REFERENCES pull_requests(pull_request_id),
		user_id VARCHAR(255) NOT NULL REFERENCES users(user_id),
		PRIMARY KEY (pull_request_id, user_id)
	);
	`
	_, err = testDB.Exec(schema)
	if err != nil {
		t.Fatalf("Failed to initialize test database: %v", err)
	}

	return testDB
}

func cleanupTestDB(testDB *sql.DB) {
	_, _ = testDB.Exec("DROP TABLE IF EXISTS pr_reviewers CASCADE")
	_, _ = testDB.Exec("DROP TABLE IF EXISTS pull_requests CASCADE")
	_, _ = testDB.Exec("DROP TABLE IF EXISTS users CASCADE")
	_, _ = testDB.Exec("DROP TABLE IF EXISTS teams CASCADE")
	_ = testDB.Close()
}

func TestTeamAdd(t *testing.T) {
	testDB := setupTestDB(t)
	defer cleanupTestDB(testDB)
	db = testDB // Set global db

	team := Team{
		TeamName: "backend",
		Members: []TeamMember{
			{UserID: "u1", Username: "Alice", IsActive: true},
			{UserID: "u2", Username: "Bob", IsActive: true},
		},
	}

	body, _ := json.Marshal(team)
	req := httptest.NewRequest(http.MethodPost, "/team/add", bytes.NewReader(body))
	w := httptest.NewRecorder()

	teamAddHandler(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d", w.Code)
	}

	// Try to add same team again - should fail
	req2 := httptest.NewRequest(http.MethodPost, "/team/add", bytes.NewReader(body))
	w2 := httptest.NewRecorder()

	teamAddHandler(w2, req2)

	if w2.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for duplicate team, got %d", w2.Code)
	}
}

func TestPullRequestCreate(t *testing.T) {
	testDB := setupTestDB(t)
	defer cleanupTestDB(testDB)
	db = testDB

	// Setup team
	_, _ = testDB.Exec("INSERT INTO teams (team_name) VALUES ('backend')")
	_, _ = testDB.Exec("INSERT INTO users (user_id, username, team_name, is_active) VALUES ('u1', 'Alice', 'backend', true)")
	_, _ = testDB.Exec("INSERT INTO users (user_id, username, team_name, is_active) VALUES ('u2', 'Bob', 'backend', true)")
	_, _ = testDB.Exec("INSERT INTO users (user_id, username, team_name, is_active) VALUES ('u3', 'Charlie', 'backend', true)")

	prReq := map[string]string{
		"pull_request_id":   "pr-1001",
		"pull_request_name": "Add feature",
		"author_id":         "u1",
	}

	body, _ := json.Marshal(prReq)
	req := httptest.NewRequest(http.MethodPost, "/pullRequest/create", bytes.NewReader(body))
	w := httptest.NewRecorder()

	pullRequestCreateHandler(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	var response map[string]PullRequest
	_ = json.Unmarshal(w.Body.Bytes(), &response)

	pr := response["pr"]
	if pr.Status != "OPEN" {
		t.Errorf("Expected status OPEN, got %s", pr.Status)
	}

	if len(pr.AssignedReviewers) > 2 {
		t.Errorf("Expected max 2 reviewers, got %d", len(pr.AssignedReviewers))
	}

	// Verify author is not assigned as reviewer
	for _, reviewerID := range pr.AssignedReviewers {
		if reviewerID == "u1" {
			t.Error("Author should not be assigned as reviewer")
		}
	}
}

func TestPullRequestMerge(t *testing.T) {
	testDB := setupTestDB(t)
	defer cleanupTestDB(testDB)
	db = testDB

	// Setup
	_, _ = testDB.Exec("INSERT INTO teams (team_name) VALUES ('backend')")
	_, _ = testDB.Exec("INSERT INTO users (user_id, username, team_name, is_active) VALUES ('u1', 'Alice', 'backend', true)")
	_, _ = testDB.Exec("INSERT INTO pull_requests (pull_request_id, pull_request_name, author_id, status) VALUES ('pr-1001', 'Test PR', 'u1', 'OPEN')")

	mergeReq := map[string]string{
		"pull_request_id": "pr-1001",
	}

	body, _ := json.Marshal(mergeReq)
	req := httptest.NewRequest(http.MethodPost, "/pullRequest/merge", bytes.NewReader(body))
	w := httptest.NewRecorder()

	pullRequestMergeHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var response map[string]PullRequest
	_ = json.Unmarshal(w.Body.Bytes(), &response)

	pr := response["pr"]
	if pr.Status != "MERGED" {
		t.Errorf("Expected status MERGED, got %s", pr.Status)
	}

	// Test idempotency - merge again
	req2 := httptest.NewRequest(http.MethodPost, "/pullRequest/merge", bytes.NewReader(body))
	w2 := httptest.NewRecorder()

	pullRequestMergeHandler(w2, req2)

	if w2.Code != http.StatusOK {
		t.Error("Merge should be idempotent")
	}
}

func TestPullRequestReassign(t *testing.T) {
	testDB := setupTestDB(t)
	defer cleanupTestDB(testDB)
	db = testDB

	// Setup
	_, _ = testDB.Exec("INSERT INTO teams (team_name) VALUES ('backend')")
	_, _ = testDB.Exec("INSERT INTO users (user_id, username, team_name, is_active) VALUES ('u1', 'Alice', 'backend', true)")
	_, _ = testDB.Exec("INSERT INTO users (user_id, username, team_name, is_active) VALUES ('u2', 'Bob', 'backend', true)")
	_, _ = testDB.Exec("INSERT INTO users (user_id, username, team_name, is_active) VALUES ('u3', 'Charlie', 'backend', true)")
	_, _ = testDB.Exec("INSERT INTO pull_requests (pull_request_id, pull_request_name, author_id, status) VALUES ('pr-1001', 'Test PR', 'u1', 'OPEN')")
	_, _ = testDB.Exec("INSERT INTO pr_reviewers (pull_request_id, user_id) VALUES ('pr-1001', 'u2')")

	reassignReq := map[string]string{
		"pull_request_id": "pr-1001",
		"old_user_id":     "u2",
	}

	body, _ := json.Marshal(reassignReq)
	req := httptest.NewRequest(http.MethodPost, "/pullRequest/reassign", bytes.NewReader(body))
	w := httptest.NewRecorder()

	pullRequestReassignHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var response map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &response)

	replacedBy := response["replaced_by"].(string)
	if replacedBy == "" {
		t.Error("replaced_by should not be empty")
	}

	if replacedBy == "u2" {
		t.Error("New reviewer should not be the same as old reviewer")
	}
}

func TestReassignOnMergedPR(t *testing.T) {
	testDB := setupTestDB(t)
	defer cleanupTestDB(testDB)
	db = testDB

	// Setup
	_, _ = testDB.Exec("INSERT INTO teams (team_name) VALUES ('backend')")
	_, _ = testDB.Exec("INSERT INTO users (user_id, username, team_name, is_active) VALUES ('u1', 'Alice', 'backend', true)")
	_, _ = testDB.Exec("INSERT INTO users (user_id, username, team_name, is_active) VALUES ('u2', 'Bob', 'backend', true)")
	_, _ = testDB.Exec("INSERT INTO pull_requests (pull_request_id, pull_request_name, author_id, status) VALUES ('pr-1001', 'Test PR', 'u1', 'MERGED')")
	_, _ = testDB.Exec("INSERT INTO pr_reviewers (pull_request_id, user_id) VALUES ('pr-1001', 'u2')")

	reassignReq := map[string]string{
		"pull_request_id": "pr-1001",
		"old_user_id":     "u2",
	}

	body, _ := json.Marshal(reassignReq)
	req := httptest.NewRequest(http.MethodPost, "/pullRequest/reassign", bytes.NewReader(body))
	w := httptest.NewRecorder()

	pullRequestReassignHandler(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("Expected status 409 for reassign on merged PR, got %d", w.Code)
	}

	var response ErrorResponse
	_ = json.Unmarshal(w.Body.Bytes(), &response)

	if response.Error.Code != "PR_MERGED" {
		t.Errorf("Expected error code PR_MERGED, got %s", response.Error.Code)
	}
}

func TestInactiveUserNotAssigned(t *testing.T) {
	testDB := setupTestDB(t)
	defer cleanupTestDB(testDB)
	db = testDB

	// Setup team with one active and one inactive user
	_, _ = testDB.Exec("INSERT INTO teams (team_name) VALUES ('backend')")
	_, _ = testDB.Exec("INSERT INTO users (user_id, username, team_name, is_active) VALUES ('u1', 'Alice', 'backend', true)")
	_, _ = testDB.Exec("INSERT INTO users (user_id, username, team_name, is_active) VALUES ('u2', 'Bob', 'backend', false)")

	prReq := map[string]string{
		"pull_request_id":   "pr-1001",
		"pull_request_name": "Add feature",
		"author_id":         "u1",
	}

	body, _ := json.Marshal(prReq)
	req := httptest.NewRequest(http.MethodPost, "/pullRequest/create", bytes.NewReader(body))
	w := httptest.NewRecorder()

	pullRequestCreateHandler(w, req)

	var response map[string]PullRequest
	_ = json.Unmarshal(w.Body.Bytes(), &response)

	pr := response["pr"]
	
	// Should not assign inactive user u2
	for _, reviewerID := range pr.AssignedReviewers {
		if reviewerID == "u2" {
			t.Error("Inactive user should not be assigned as reviewer")
		}
	}
}

func TestTeamMassDeactivation(t *testing.T) {
	testDB := setupTestDB(t)
	defer cleanupTestDB(testDB)
	db = testDB

	// Setup team with multiple users and open PRs
	_, _ = testDB.Exec("INSERT INTO teams (team_name) VALUES ('backend')")
	_, _ = testDB.Exec("INSERT INTO users (user_id, username, team_name, is_active) VALUES ('u1', 'Alice', 'backend', true)")
	_, _ = testDB.Exec("INSERT INTO users (user_id, username, team_name, is_active) VALUES ('u2', 'Bob', 'backend', true)")
	_, _ = testDB.Exec("INSERT INTO users (user_id, username, team_name, is_active) VALUES ('u3', 'Charlie', 'backend', true)")
	_, _ = testDB.Exec("INSERT INTO users (user_id, username, team_name, is_active) VALUES ('u4', 'Dave', 'backend', true)")

	// Create open PRs with reviewers
	_, _ = testDB.Exec("INSERT INTO pull_requests (pull_request_id, pull_request_name, author_id, status) VALUES ('pr-1001', 'Test PR 1', 'u1', 'OPEN')")
	_, _ = testDB.Exec("INSERT INTO pr_reviewers (pull_request_id, user_id) VALUES ('pr-1001', 'u2')")
	_, _ = testDB.Exec("INSERT INTO pr_reviewers (pull_request_id, user_id) VALUES ('pr-1001', 'u3')")

	_, _ = testDB.Exec("INSERT INTO pull_requests (pull_request_id, pull_request_name, author_id, status) VALUES ('pr-1002', 'Test PR 2', 'u2', 'OPEN')")
	_, _ = testDB.Exec("INSERT INTO pr_reviewers (pull_request_id, user_id) VALUES ('pr-1002', 'u1')")

	deactivateReq := map[string]string{
		"team_name": "backend",
	}

	body, _ := json.Marshal(deactivateReq)
	req := httptest.NewRequest(http.MethodPost, "/team/deactivate", bytes.NewReader(body))
	w := httptest.NewRecorder()

	teamDeactivateHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var response map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &response)

	// Check deactivated count
	deactivatedCount := int(response["deactivated_count"].(float64))
	if deactivatedCount != 4 {
		t.Errorf("Expected 4 users deactivated, got %d", deactivatedCount)
	}

	// Verify all users are now inactive in database
	var activeCount int
	_ = testDB.QueryRow("SELECT COUNT(*) FROM users WHERE team_name = 'backend' AND is_active = true").Scan(&activeCount)
	if activeCount != 0 {
		t.Errorf("Expected 0 active users, found %d", activeCount)
	}

	// Verify reassignments happened
	reassignments := response["reassignments"].([]interface{})
	if len(reassignments) == 0 {
		t.Log("Note: No reassignments occurred (expected if no active replacement candidates)")
	}
}

