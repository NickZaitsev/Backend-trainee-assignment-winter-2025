#!/bin/bash

# Load Testing Script for PR Reviewer Assignment Service
# This script performs load testing to verify performance requirements:
# - RPS: 5
# - Response time SLI: 300ms
# - Success rate SLI: 99.9%

echo "PR Reviewer Assignment Service - Load Testing"
echo "=============================================="
echo ""

# Configuration
BASE_URL="${BASE_URL:-http://localhost:8080}"
TOTAL_REQUESTS=300  # 60 seconds * 5 RPS
CONCURRENT=5
TEST_DURATION=60

# Check if curl is available
if ! command -v curl &> /dev/null; then
    echo "Error: curl is required but not installed"
    exit 1
fi

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Initialize counters
success_count=0
fail_count=0
total_time=0

# Create test data
echo "Setting up test data..."

# Create teams
curl -s -X POST "$BASE_URL/team/add" \
  -H "Content-Type: application/json" \
  -d '{
    "team_name": "loadtest_backend",
    "members": [
      {"user_id": "lt_u1", "username": "LoadTest User 1", "is_active": true},
      {"user_id": "lt_u2", "username": "LoadTest User 2", "is_active": true},
      {"user_id": "lt_u3", "username": "LoadTest User 3", "is_active": true},
      {"user_id": "lt_u4", "username": "LoadTest User 4", "is_active": true},
      {"user_id": "lt_u5", "username": "LoadTest User 5", "is_active": true}
    ]
  }' > /dev/null

echo "Test data created"
echo ""
echo "Starting load test..."
echo "Target: $TOTAL_REQUESTS requests over $TEST_DURATION seconds (~5 RPS)"
echo "Max concurrent requests: $CONCURRENT"
echo ""

# Function to create PR and measure time
test_create_pr() {
    local id=$1
    local start=$(date +%s%N)
    
    local response=$(curl -s -w "\n%{http_code}" -X POST "$BASE_URL/pullRequest/create" \
      -H "Content-Type: application/json" \
      -d "{
        \"pull_request_id\": \"loadtest-pr-$id\",
        \"pull_request_name\": \"Load Test PR $id\",
        \"author_id\": \"lt_u1\"
      }")
    
    local end=$(date +%s%N)
    local duration=$(( (end - start) / 1000000 ))  # Convert to milliseconds
    
    local http_code=$(echo "$response" | tail -n1)
    
    if [ "$http_code" = "201" ] || [ "$http_code" = "409" ]; then
        echo "$duration"
        return 0
    else
        echo "0"
        return 1
    fi
}

# Function to test various endpoints
test_endpoint() {
    local endpoint=$1
    local method=$2
    local data=$3
    
    local start=$(date +%s%N)
    
    if [ "$method" = "GET" ]; then
        local response=$(curl -s -w "\n%{http_code}" -X GET "$BASE_URL$endpoint")
    else
        local response=$(curl -s -w "\n%{http_code}" -X POST "$BASE_URL$endpoint" \
          -H "Content-Type: application/json" \
          -d "$data")
    fi
    
    local end=$(date +%s%N)
    local duration=$(( (end - start) / 1000000 ))
    
    local http_code=$(echo "$response" | tail -n1)
    
    if [ "$http_code" -ge 200 ] && [ "$http_code" -lt 500 ]; then
        echo "$duration"
        return 0
    else
        echo "0"
        return 1
    fi
}

# Arrays to store response times
declare -a response_times

# Run load test
start_time=$(date +%s)
request_interval=$(echo "scale=3; $TEST_DURATION / $TOTAL_REQUESTS" | bc)

for i in $(seq 1 $TOTAL_REQUESTS); do
    iter_start=$(date +%s%N)
    
    # Mix different types of requests
    case $((i % 5)) in
        0)
            # Create PR
            duration=$(test_create_pr $i)
            ;;
        1)
            # Get team
            duration=$(test_endpoint "/team/get?team_name=loadtest_backend" "GET" "")
            ;;
        2)
            # Get stats
            duration=$(test_endpoint "/stats" "GET" "")
            ;;
        3)
            # Get user reviews
            duration=$(test_endpoint "/users/getReview?user_id=lt_u2" "GET" "")
            ;;
        4)
            # Health check
            duration=$(test_endpoint "/health" "GET" "")
            ;;
    esac
    
    if [ $? -eq 0 ] && [ "$duration" != "0" ]; then
        ((success_count++))
        response_times+=($duration)
        total_time=$((total_time + duration))
        
        # Progress indicator
        if [ $((i % 50)) -eq 0 ]; then
            echo "Progress: $i/$TOTAL_REQUESTS requests completed"
        fi
    else
        ((fail_count++))
    fi
    
    # Rate limiting - sleep to maintain target RPS
    iter_end=$(date +%s%N)
    elapsed=$(echo "scale=3; ($iter_end - $iter_start) / 1000000000" | bc)
    sleep_time=$(echo "scale=3; $request_interval - $elapsed" | bc)
    
    if (( $(echo "$sleep_time > 0" | bc -l) )); then
        sleep $sleep_time
    fi
done

end_time=$(date +%s)
actual_duration=$((end_time - start_time))

# Calculate statistics
total_requests=$((success_count + fail_count))
success_rate=$(echo "scale=2; $success_count * 100 / $total_requests" | bc)
actual_rps=$(echo "scale=2; $total_requests / $actual_duration" | bc)

if [ $success_count -gt 0 ]; then
    avg_response_time=$((total_time / success_count))
    
    # Calculate p50, p95, p99
    IFS=$'\n' sorted=($(sort -n <<<"${response_times[*]}"))
    unset IFS
    
    count=${#sorted[@]}
    p50_idx=$((count * 50 / 100))
    p95_idx=$((count * 95 / 100))
    p99_idx=$((count * 99 / 100))
    
    p50=${sorted[$p50_idx]}
    p95=${sorted[$p95_idx]}
    p99=${sorted[$p99_idx]}
else
    avg_response_time=0
    p50=0
    p95=0
    p99=0
fi

# Print results
echo ""
echo "=============================================="
echo "Load Test Results"
echo "=============================================="
echo ""
echo "Test Duration: ${actual_duration}s"
echo "Total Requests: $total_requests"
echo "Successful: $success_count"
echo "Failed: $fail_count"
echo ""
echo "Success Rate: ${success_rate}%"
echo "Actual RPS: $actual_rps"
echo ""
echo "Response Times (ms):"
echo "  Average: ${avg_response_time}ms"
echo "  P50 (median): ${p50}ms"
echo "  P95: ${p95}ms"
echo "  P99: ${p99}ms"
echo ""
echo "Performance Requirements:"

# Check success rate SLI (99.9%)
if (( $(echo "$success_rate >= 99.9" | bc -l) )); then
    echo -e "  ${GREEN}✓${NC} Success Rate SLI (99.9%): PASS (${success_rate}%)"
else
    echo -e "  ${RED}✗${NC} Success Rate SLI (99.9%): FAIL (${success_rate}%)"
fi

# Check response time SLI (300ms average)
if [ $avg_response_time -le 300 ]; then
    echo -e "  ${GREEN}✓${NC} Response Time SLI (300ms): PASS (${avg_response_time}ms avg)"
else
    echo -e "  ${YELLOW}!${NC} Response Time SLI (300ms): WARNING (${avg_response_time}ms avg)"
fi

# Check RPS
if (( $(echo "$actual_rps >= 5" | bc -l) )); then
    echo -e "  ${GREEN}✓${NC} RPS Target (5): PASS (${actual_rps} RPS)"
else
    echo -e "  ${RED}✗${NC} RPS Target (5): INFO (${actual_rps} RPS)"
fi

echo ""
echo "=============================================="
