// Package main implements a Pull Request review management system API.
// It provides endpoints for managing teams, users, and pull request reviews with automatic reviewer assignment.
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"crypto/rand"
	"log"
	"math/big"
	"net/http"
	"os"
	"time"

	_ "github.com/lib/pq"
)

type ErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type TeamMember struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	IsActive bool   `json:"is_active"`
}

type Team struct {
	TeamName string       `json:"team_name"`
	Members  []TeamMember `json:"members"`
}

type User struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	TeamName string `json:"team_name"`
	IsActive bool   `json:"is_active"`
}

type PullRequest struct {
	PullRequestID     string    `json:"pull_request_id"`
	PullRequestName   string    `json:"pull_request_name"`
	AuthorID          string    `json:"author_id"`
	Status            string    `json:"status"`
	AssignedReviewers []string  `json:"assigned_reviewers"`
	CreatedAt         *string   `json:"createdAt,omitempty"`
	MergedAt          *string   `json:"mergedAt,omitempty"`
}

type PullRequestShort struct {
	PullRequestID   string `json:"pull_request_id"`
	PullRequestName string `json:"pull_request_name"`
	AuthorID        string `json:"author_id"`
	Status          string `json:"status"`
}

var db *sql.DB

func main() {
	var err error
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://user:password@localhost:5432/avito?sslmode=disable"
	}

	// Wait for database to be ready and connect
	for i := 0; i < 30; i++ {
		db, err = sql.Open("postgres", databaseURL)
		if err == nil {
			err = db.Ping()
			if err == nil {
				break
			}
		}
		log.Printf("Waiting for database... (%d/30)", i+1)
		time.Sleep(2 * time.Second)
	}

	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("Error closing database: %v", err)
		}
	}()

	// Initialize database schema
	initDB()

	// Setup routes
	http.HandleFunc("/team/add", teamAddHandler)
	http.HandleFunc("/team/get", teamGetHandler)
	http.HandleFunc("/users/setIsActive", usersSetIsActiveHandler)
	http.HandleFunc("/pullRequest/create", pullRequestCreateHandler)
	http.HandleFunc("/pullRequest/merge", pullRequestMergeHandler)
	http.HandleFunc("/pullRequest/reassign", pullRequestReassignHandler)
	http.HandleFunc("/users/getReview", usersGetReviewHandler)
	
	// Bonus endpoints
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/stats", statsHandler)
	http.HandleFunc("/team/deactivate", teamDeactivateHandler)

	log.Println("Server starting on :8080")
	
	// Create server with timeouts for security
	server := &http.Server{
		Addr:         ":8080",
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}

func initDB() {
	schema := `
	CREATE TABLE IF NOT EXISTS teams (
		team_name VARCHAR(255) PRIMARY KEY
	);

	CREATE TABLE IF NOT EXISTS users (
		user_id VARCHAR(255) PRIMARY KEY,
		username VARCHAR(255) NOT NULL,
		team_name VARCHAR(255) NOT NULL REFERENCES teams(team_name),
		is_active BOOLEAN NOT NULL DEFAULT true
	);

	CREATE TABLE IF NOT EXISTS pull_requests (
		pull_request_id VARCHAR(255) PRIMARY KEY,
		pull_request_name VARCHAR(255) NOT NULL,
		author_id VARCHAR(255) NOT NULL REFERENCES users(user_id),
		status VARCHAR(10) NOT NULL CHECK (status IN ('OPEN', 'MERGED')),
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		merged_at TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS pr_reviewers (
		pull_request_id VARCHAR(255) NOT NULL REFERENCES pull_requests(pull_request_id),
		user_id VARCHAR(255) NOT NULL REFERENCES users(user_id),
		PRIMARY KEY (pull_request_id, user_id)
	);
	`
	_, err := db.Exec(schema)
	if err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	log.Println("Database initialized successfully")
}

func teamAddHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var team Team
	if err := json.NewDecoder(r.Body).Decode(&team); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Check if team already exists
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM teams WHERE team_name = $1)", team.TeamName).Scan(&exists)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if exists {
		sendError(w, http.StatusBadRequest, "TEAM_EXISTS", "team_name already exists")
		return
	}

	// Create team
	_, err = db.Exec("INSERT INTO teams (team_name) VALUES ($1)", team.TeamName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Insert or update users
	for _, member := range team.Members {
		_, err = db.Exec(`
			INSERT INTO users (user_id, username, team_name, is_active)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (user_id) DO UPDATE 
			SET username = $2, team_name = $3, is_active = $4
		`, member.UserID, member.Username, team.TeamName, member.IsActive)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"team": team}); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

func teamGetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	teamName := r.URL.Query().Get("team_name")
	if teamName == "" {
		http.Error(w, "team_name is required", http.StatusBadRequest)
		return
	}

	// Check if team exists
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM teams WHERE team_name = $1)", teamName).Scan(&exists)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !exists {
		sendError(w, http.StatusNotFound, "NOT_FOUND", "team not found")
		return
	}

	// Get team members
	rows, err := db.Query("SELECT user_id, username, is_active FROM users WHERE team_name = $1", teamName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}()

	var members []TeamMember
	for rows.Next() {
		var member TeamMember
		if err := rows.Scan(&member.UserID, &member.Username, &member.IsActive); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		members = append(members, member)
	}

	team := Team{
		TeamName: teamName,
		Members:  members,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(team); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

func usersSetIsActiveHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		UserID   string `json:"user_id"`
		IsActive bool   `json:"is_active"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Update user
	result, err := db.Exec("UPDATE users SET is_active = $1 WHERE user_id = $2", req.IsActive, req.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		sendError(w, http.StatusNotFound, "NOT_FOUND", "user not found")
		return
	}

	// Get updated user info
	var user User
	err = db.QueryRow("SELECT user_id, username, team_name, is_active FROM users WHERE user_id = $1", req.UserID).
		Scan(&user.UserID, &user.Username, &user.TeamName, &user.IsActive)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"user": user}); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

func pullRequestCreateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PullRequestID   string `json:"pull_request_id"`
		PullRequestName string `json:"pull_request_name"`
		AuthorID        string `json:"author_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Check if PR already exists
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM pull_requests WHERE pull_request_id = $1)", req.PullRequestID).Scan(&exists)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if exists {
		sendError(w, http.StatusConflict, "PR_EXISTS", "PR id already exists")
		return
	}

	// Get author's team
	var authorTeam string
	err = db.QueryRow("SELECT team_name FROM users WHERE user_id = $1", req.AuthorID).Scan(&authorTeam)
	if err != nil {
		if err == sql.ErrNoRows {
			sendError(w, http.StatusNotFound, "NOT_FOUND", "author not found")
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Create PR
	var createdAt time.Time
	err = db.QueryRow(`
		INSERT INTO pull_requests (pull_request_id, pull_request_name, author_id, status, created_at)
		VALUES ($1, $2, $3, 'OPEN', CURRENT_TIMESTAMP)
		RETURNING created_at
	`, req.PullRequestID, req.PullRequestName, req.AuthorID).Scan(&createdAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get active team members (excluding author) for reviewer assignment
	reviewers := getActiveTeamMembers(authorTeam, req.AuthorID)

	// Assign up to 2 reviewers randomly
	assignedReviewers := assignReviewers(reviewers, 2)

	// Insert reviewers
	for _, reviewerID := range assignedReviewers {
		_, err = db.Exec("INSERT INTO pr_reviewers (pull_request_id, user_id) VALUES ($1, $2)", req.PullRequestID, reviewerID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	createdAtStr := createdAt.Format(time.RFC3339)
	pr := PullRequest{
		PullRequestID:     req.PullRequestID,
		PullRequestName:   req.PullRequestName,
		AuthorID:          req.AuthorID,
		Status:            "OPEN",
		AssignedReviewers: assignedReviewers,
		CreatedAt:         &createdAtStr,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"pr": pr}); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

func pullRequestMergeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PullRequestID string `json:"pull_request_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Check if PR exists
	var status string
	var mergedAt sql.NullTime
	err := db.QueryRow("SELECT status, merged_at FROM pull_requests WHERE pull_request_id = $1", req.PullRequestID).Scan(&status, &mergedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			sendError(w, http.StatusNotFound, "NOT_FOUND", "PR not found")
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Idempotent: if already merged, return current state
	if status == "MERGED" {
		pr := getPullRequest(req.PullRequestID)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{"pr": pr}); err != nil {
			log.Printf("Error encoding response: %v", err)
		}
		return
	}

	// Update PR to MERGED
	var newMergedAt time.Time
	err = db.QueryRow("UPDATE pull_requests SET status = 'MERGED', merged_at = CURRENT_TIMESTAMP WHERE pull_request_id = $1 RETURNING merged_at", req.PullRequestID).Scan(&newMergedAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	pr := getPullRequest(req.PullRequestID)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"pr": pr}); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

func pullRequestReassignHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PullRequestID string `json:"pull_request_id"`
		OldUserID     string `json:"old_user_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Check if PR exists and get status
	var status string
	err := db.QueryRow("SELECT status FROM pull_requests WHERE pull_request_id = $1", req.PullRequestID).Scan(&status)
	if err != nil {
		if err == sql.ErrNoRows {
			sendError(w, http.StatusNotFound, "NOT_FOUND", "PR not found")
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Check if PR is merged
	if status == "MERGED" {
		sendError(w, http.StatusConflict, "PR_MERGED", "cannot reassign on merged PR")
		return
	}

	// Check if old user is assigned as reviewer
	var isAssigned bool
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM pr_reviewers WHERE pull_request_id = $1 AND user_id = $2)", req.PullRequestID, req.OldUserID).Scan(&isAssigned)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !isAssigned {
		sendError(w, http.StatusConflict, "NOT_ASSIGNED", "reviewer is not assigned to this PR")
		return
	}

	// Get old reviewer's team
	var oldReviewerTeam string
	err = db.QueryRow("SELECT team_name FROM users WHERE user_id = $1", req.OldUserID).Scan(&oldReviewerTeam)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get author ID to exclude from candidates
	var authorID string
	err = db.QueryRow("SELECT author_id FROM pull_requests WHERE pull_request_id = $1", req.PullRequestID).Scan(&authorID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get currently assigned reviewers to exclude
	currentReviewers := getCurrentReviewers(req.PullRequestID)

	// Get active team members from old reviewer's team (excluding author and current reviewers)
	candidates := getActiveTeamMembersExcluding(oldReviewerTeam, append(currentReviewers, authorID))

	if len(candidates) == 0 {
		sendError(w, http.StatusConflict, "NO_CANDIDATE", "no active replacement candidate in team")
		return
	}

	// Randomly select a new reviewer using crypto/rand for security
	newReviewerID, err := selectRandomCandidate(candidates)
	if err != nil {
		http.Error(w, "Failed to select reviewer", http.StatusInternalServerError)
		return
	}

	// Replace reviewer
	_, err = db.Exec("UPDATE pr_reviewers SET user_id = $1 WHERE pull_request_id = $2 AND user_id = $3", newReviewerID, req.PullRequestID, req.OldUserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	pr := getPullRequest(req.PullRequestID)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"pr":          pr,
		"replaced_by": newReviewerID,
	}); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

func usersGetReviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		http.Error(w, "user_id is required", http.StatusBadRequest)
		return
	}

	// Get PRs where user is a reviewer
	rows, err := db.Query(`
		SELECT pr.pull_request_id, pr.pull_request_name, pr.author_id, pr.status
		FROM pull_requests pr
		JOIN pr_reviewers r ON pr.pull_request_id = r.pull_request_id
		WHERE r.user_id = $1
	`, userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}()

	var pullRequests []PullRequestShort
	for rows.Next() {
		var pr PullRequestShort
		if err := rows.Scan(&pr.PullRequestID, &pr.PullRequestName, &pr.AuthorID, &pr.Status); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		pullRequests = append(pullRequests, pr)
	}

	if pullRequests == nil {
		pullRequests = []PullRequestShort{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"user_id":       userID,
		"pull_requests": pullRequests,
	}); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

// Bonus endpoints

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check database connection
	if err := db.Ping(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		if encodeErr := json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "unhealthy",
			"error":  err.Error(),
		}); encodeErr != nil {
			log.Printf("Error encoding response: %v", encodeErr)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
	}); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

func statsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type UserStats struct {
		UserID         string `json:"user_id"`
		Username       string `json:"username"`
		ReviewCount    int    `json:"review_count"`
		AuthoredPRs    int    `json:"authored_prs"`
		OpenReviews    int    `json:"open_reviews"`
		MergedReviews  int    `json:"merged_reviews"`
	}

	type Stats struct {
		TotalTeams       int         `json:"total_teams"`
		TotalUsers       int         `json:"total_users"`
		ActiveUsers      int         `json:"active_users"`
		TotalPRs         int         `json:"total_prs"`
		OpenPRs          int         `json:"open_prs"`
		MergedPRs        int         `json:"merged_prs"`
		TopReviewers     []UserStats `json:"top_reviewers"`
	}

	var stats Stats

	// Get totals
	if err := db.QueryRow("SELECT COUNT(*) FROM teams").Scan(&stats.TotalTeams); err != nil {
		log.Printf("Error getting total teams: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&stats.TotalUsers); err != nil {
		log.Printf("Error getting total users: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM users WHERE is_active = true").Scan(&stats.ActiveUsers); err != nil {
		log.Printf("Error getting active users: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM pull_requests").Scan(&stats.TotalPRs); err != nil {
		log.Printf("Error getting total PRs: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM pull_requests WHERE status = 'OPEN'").Scan(&stats.OpenPRs); err != nil {
		log.Printf("Error getting open PRs: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM pull_requests WHERE status = 'MERGED'").Scan(&stats.MergedPRs); err != nil {
		log.Printf("Error getting merged PRs: %v", err)
	}

	// Get top reviewers
	rows, err := db.Query(`
		SELECT 
			u.user_id,
			u.username,
			COUNT(DISTINCT r.pull_request_id) as review_count,
			COUNT(DISTINCT pr_authored.pull_request_id) as authored_prs,
			COUNT(DISTINCT CASE WHEN pr.status = 'OPEN' THEN r.pull_request_id END) as open_reviews,
			COUNT(DISTINCT CASE WHEN pr.status = 'MERGED' THEN r.pull_request_id END) as merged_reviews
		FROM users u
		LEFT JOIN pr_reviewers r ON u.user_id = r.user_id
		LEFT JOIN pull_requests pr ON r.pull_request_id = pr.pull_request_id
		LEFT JOIN pull_requests pr_authored ON u.user_id = pr_authored.author_id
		GROUP BY u.user_id, u.username
		ORDER BY review_count DESC
		LIMIT 10
	`)
	if err == nil {
		defer func() {
			if err := rows.Close(); err != nil {
				log.Printf("Error closing rows: %v", err)
			}
		}()
		for rows.Next() {
			var us UserStats
			if err := rows.Scan(&us.UserID, &us.Username, &us.ReviewCount, &us.AuthoredPRs, &us.OpenReviews, &us.MergedReviews); err != nil {
				log.Printf("Error scanning user stats: %v", err)
				continue
			}
			stats.TopReviewers = append(stats.TopReviewers, us)
		}
	}

	if stats.TopReviewers == nil {
		stats.TopReviewers = []UserStats{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

// teamDeactivateHandler handles mass deactivation of team members and reassigns their open PRs
func teamDeactivateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		TeamName string `json:"team_name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Check if team exists
	var exists bool
	err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM teams WHERE team_name = $1)", req.TeamName).Scan(&exists)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !exists {
		sendError(w, http.StatusNotFound, "NOT_FOUND", "team not found")
		return
	}

	// Start transaction for atomicity
	tx, err := db.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
			log.Printf("Error rolling back transaction: %v", err)
		}
	}()

	// Get all active users in the team
	rows, err := tx.Query("SELECT user_id FROM users WHERE team_name = $1 AND is_active = true", req.TeamName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}()

	var usersToDeactivate []string
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		usersToDeactivate = append(usersToDeactivate, userID)
	}

	// Track reassignments for response
	type Reassignment struct {
		PRID        string `json:"pr_id"`
		OldReviewer string `json:"old_reviewer"`
		NewReviewer string `json:"new_reviewer"`
	}
	var reassignments []Reassignment
	var failedReassignments []string

	// Process each user's open PR reviews
	for _, userID := range usersToDeactivate {
		// Get all OPEN PRs where this user is a reviewer
		prRows, err := tx.Query(`
			SELECT pr.pull_request_id, pr.author_id
			FROM pull_requests pr
			JOIN pr_reviewers r ON pr.pull_request_id = r.pull_request_id
			WHERE r.user_id = $1 AND pr.status = 'OPEN'
		`, userID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
		var prsToReassign []struct {
			PRID     string
			AuthorID string
		}
		func() {
			defer func() {
				if err := prRows.Close(); err != nil {
					log.Printf("Error closing rows: %v", err)
				}
			}()

			for prRows.Next() {
				var pr struct {
					PRID     string
					AuthorID string
				}
				if err := prRows.Scan(&pr.PRID, &pr.AuthorID); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				prsToReassign = append(prsToReassign, pr)
			}
		}()

		// Reassign each PR
		for _, pr := range prsToReassign {
			// Get current reviewers for this PR
			var currentReviewers []string
			func() {
				revRows, err := tx.Query("SELECT user_id FROM pr_reviewers WHERE pull_request_id = $1", pr.PRID)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				defer func() {
					if err := revRows.Close(); err != nil {
						log.Printf("Error closing rows: %v", err)
					}
				}()

				for revRows.Next() {
					var revID string
					if err := revRows.Scan(&revID); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					currentReviewers = append(currentReviewers, revID)
				}
			}()

			// Find replacement from the same team (excluding current reviewers and author)
			excludeList := append(currentReviewers, pr.AuthorID)
			
			// Build query to find active replacement from same team
			query := `SELECT user_id FROM users 
				WHERE team_name = $1 AND is_active = true AND user_id != ALL($2)
				LIMIT 1`
			
			var newReviewerID string
			err = tx.QueryRow(query, req.TeamName, excludeList).Scan(&newReviewerID)
			
			if err == sql.ErrNoRows {
				// No replacement available - just remove the reviewer
				_, err = tx.Exec("DELETE FROM pr_reviewers WHERE pull_request_id = $1 AND user_id = $2", pr.PRID, userID)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				failedReassignments = append(failedReassignments, pr.PRID)
			} else if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			} else {
				// Replace the reviewer
				_, err = tx.Exec("UPDATE pr_reviewers SET user_id = $1 WHERE pull_request_id = $2 AND user_id = $3", 
					newReviewerID, pr.PRID, userID)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				reassignments = append(reassignments, Reassignment{
					PRID:        pr.PRID,
					OldReviewer: userID,
					NewReviewer: newReviewerID,
				})
			}
		}
	}

	// Deactivate all users in the team
	result, err := tx.Exec("UPDATE users SET is_active = false WHERE team_name = $1", req.TeamName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	deactivatedCount, _ := result.RowsAffected()

	// Commit transaction
	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"team_name":             req.TeamName,
		"deactivated_count":     deactivatedCount,
		"reassignments":         reassignments,
		"failed_reassignments":  failedReassignments,
	}); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

// Helper functions

func sendError(w http.ResponseWriter, statusCode int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	var errResp ErrorResponse
	errResp.Error.Code = code
	errResp.Error.Message = message
	if err := json.NewEncoder(w).Encode(errResp); err != nil {
		log.Printf("Error encoding error response: %v", err)
	}
}

func getActiveTeamMembers(teamName, excludeUserID string) []string {
	rows, err := db.Query("SELECT user_id FROM users WHERE team_name = $1 AND is_active = true AND user_id != $2", teamName, excludeUserID)
	if err != nil {
		return []string{}
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}()

	var members []string
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			continue
		}
		members = append(members, userID)
	}
	return members
}

func getActiveTeamMembersExcluding(teamName string, excludeUserIDs []string) []string {
	if len(excludeUserIDs) == 0 {
		rows, err := db.Query("SELECT user_id FROM users WHERE team_name = $1 AND is_active = true", teamName)
		if err != nil {
			return []string{}
		}
		defer func() {
			if err := rows.Close(); err != nil {
				log.Printf("Error closing rows: %v", err)
			}
		}()

		var members []string
		for rows.Next() {
			var userID string
			if err := rows.Scan(&userID); err != nil {
				continue
			}
			members = append(members, userID)
		}
		return members
	}

	// Build query with placeholders for excluded IDs
	query := "SELECT user_id FROM users WHERE team_name = $1 AND is_active = true AND user_id NOT IN ("
	args := []interface{}{teamName}
	for i, id := range excludeUserIDs {
		if i > 0 {
			query += ", "
		}
		query += "$" + fmt.Sprintf("%d", i+2)
		args = append(args, id)
	}
	query += ")"

	rows, err := db.Query(query, args...)
	if err != nil {
		return []string{}
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}()

	var members []string
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			continue
		}
		members = append(members, userID)
	}
	return members
}

func assignReviewers(candidates []string, maxCount int) []string {
	if len(candidates) <= maxCount {
		return candidates
	}

	// Shuffle and take first maxCount using crypto/rand
	shuffled := make([]string, len(candidates))
	copy(shuffled, candidates)
	
	// Fisher-Yates shuffle with crypto/rand
	for i := len(shuffled) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			log.Printf("Error generating random number: %v", err)
			continue
		}
		j := n.Int64()
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}

	return shuffled[:maxCount]
}

// selectRandomCandidate selects a random candidate using crypto/rand for security
func selectRandomCandidate(candidates []string) (string, error) {
	if len(candidates) == 0 {
		return "", fmt.Errorf("no candidates available")
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(candidates))))
	if err != nil {
		return "", err
	}
	return candidates[n.Int64()], nil
}

func getCurrentReviewers(prID string) []string {
	rows, err := db.Query("SELECT user_id FROM pr_reviewers WHERE pull_request_id = $1", prID)
	if err != nil {
		return []string{}
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}()

	var reviewers []string
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			continue
		}
		reviewers = append(reviewers, userID)
	}
	return reviewers
}

func getPullRequest(prID string) PullRequest {
	var pr PullRequest
	var createdAt, mergedAt sql.NullTime

	err := db.QueryRow(`
		SELECT pull_request_id, pull_request_name, author_id, status, created_at, merged_at
		FROM pull_requests
		WHERE pull_request_id = $1
	`, prID).Scan(&pr.PullRequestID, &pr.PullRequestName, &pr.AuthorID, &pr.Status, &createdAt, &mergedAt)

	if err != nil {
		return pr
	}

	if createdAt.Valid {
		createdAtStr := createdAt.Time.Format(time.RFC3339)
		pr.CreatedAt = &createdAtStr
	}

	if mergedAt.Valid {
		mergedAtStr := mergedAt.Time.Format(time.RFC3339)
		pr.MergedAt = &mergedAtStr
	}

	pr.AssignedReviewers = getCurrentReviewers(prID)
	if pr.AssignedReviewers == nil {
		pr.AssignedReviewers = []string{}
	}

	return pr
}
