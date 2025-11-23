@echo off
REM Test script for PR Reviewer Assignment Service (Windows version)

set BASE_URL=http://localhost:8080

echo === Testing PR Reviewer Assignment Service ===
echo.

REM Test 1: Create a team
echo Test 1: Creating team 'backend'...
curl -s -X POST "%BASE_URL%/team/add" -H "Content-Type: application/json" -d "{\"team_name\": \"backend\", \"members\": [{\"user_id\": \"u1\", \"username\": \"Alice\", \"is_active\": true}, {\"user_id\": \"u2\", \"username\": \"Bob\", \"is_active\": true}, {\"user_id\": \"u3\", \"username\": \"Charlie\", \"is_active\": true}, {\"user_id\": \"u4\", \"username\": \"Dave\", \"is_active\": true}]}"
echo.
echo.

REM Test 2: Get team
echo Test 2: Getting team 'backend'...
curl -s "%BASE_URL%/team/get?team_name=backend"
echo.
echo.

REM Test 3: Create PR
echo Test 3: Creating PR...
curl -s -X POST "%BASE_URL%/pullRequest/create" -H "Content-Type: application/json" -d "{\"pull_request_id\": \"pr-1001\", \"pull_request_name\": \"Add search feature\", \"author_id\": \"u1\"}"
echo.
echo.

REM Test 4: Get user reviews
echo Test 4: Getting reviews for user u2...
curl -s "%BASE_URL%/users/getReview?user_id=u2"
echo.
echo.

REM Test 5: Merge PR
echo Test 5: Merging PR pr-1001...
curl -s -X POST "%BASE_URL%/pullRequest/merge" -H "Content-Type: application/json" -d "{\"pull_request_id\": \"pr-1001\"}"
echo.
echo.

echo === Tests completed ===
