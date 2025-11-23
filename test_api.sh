#!/bin/bash

# Test script for PR Reviewer Assignment Service

BASE_URL="http://localhost:8080"

echo "=== Testing PR Reviewer Assignment Service ==="
echo ""

# Test 1: Create a team
echo "Test 1: Creating team 'backend'..."
curl -s -X POST "$BASE_URL/team/add" \
  -H "Content-Type: application/json" \
  -d '{
    "team_name": "backend",
    "members": [
      {"user_id": "u1", "username": "Alice", "is_active": true},
      {"user_id": "u2", "username": "Bob", "is_active": true},
      {"user_id": "u3", "username": "Charlie", "is_active": true},
      {"user_id": "u4", "username": "Dave", "is_active": true}
    ]
  }' | jq .

echo ""

# Test 2: Get team
echo "Test 2: Getting team 'backend'..."
curl -s "$BASE_URL/team/get?team_name=backend" | jq .

echo ""

# Test 3: Create PR
echo "Test 3: Creating PR..."
curl -s -X POST "$BASE_URL/pullRequest/create" \
  -H "Content-Type: application/json" \
  -d '{
    "pull_request_id": "pr-1001",
    "pull_request_name": "Add search feature",
    "author_id": "u1"
  }' | jq .

echo ""

# Test 4: Get user reviews
echo "Test 4: Getting reviews for user u2..."
curl -s "$BASE_URL/users/getReview?user_id=u2" | jq .

echo ""

# Test 5: Set user inactive
echo "Test 5: Setting user u2 inactive..."
curl -s -X POST "$BASE_URL/users/setIsActive" \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "u2",
    "is_active": false
  }' | jq .

echo ""

# Test 6: Create another PR
echo "Test 6: Creating another PR..."
curl -s -X POST "$BASE_URL/pullRequest/create" \
  -H "Content-Type: application/json" \
  -d '{
    "pull_request_id": "pr-1002",
    "pull_request_name": "Fix bug",
    "author_id": "u3"
  }' | jq .

echo ""

# Test 7: Merge PR
echo "Test 7: Merging PR pr-1001..."
curl -s -X POST "$BASE_URL/pullRequest/merge" \
  -H "Content-Type: application/json" \
  -d '{
    "pull_request_id": "pr-1001"
  }' | jq .

echo ""

# Test 8: Try to reassign on merged PR (should fail)
echo "Test 8: Trying to reassign on merged PR (should fail)..."
curl -s -X POST "$BASE_URL/pullRequest/reassign" \
  -H "Content-Type: application/json" \
  -d '{
    "pull_request_id": "pr-1001",
    "old_user_id": "u2"
  }' | jq .

echo ""

echo "=== Tests completed ==="
