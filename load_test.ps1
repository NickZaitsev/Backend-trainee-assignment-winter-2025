# Load Testing Script for PR Reviewer Assignment Service (PowerShell)
# This script performs load testing to verify performance requirements

param(
    [string]$BaseUrl = "http://localhost:8080",
    [int]$TotalRequests = 300,
    [int]$TestDuration = 60
)

Write-Host "PR Reviewer Assignment Service - Load Testing" -ForegroundColor Cyan
Write-Host "==============================================" -ForegroundColor Cyan
Write-Host ""

# Initialize counters
$successCount = 0
$failCount = 0
$responseTimes = @()

# Create test data
Write-Host "Setting up test data..."

$teamData = @{
    team_name = "loadtest_backend"
    members = @(
        @{user_id="lt_u1"; username="LoadTest User 1"; is_active=$true},
        @{user_id="lt_u2"; username="LoadTest User 2"; is_active=$true},
        @{user_id="lt_u3"; username="LoadTest User 3"; is_active=$true},
        @{user_id="lt_u4"; username="LoadTest User 4"; is_active=$true},
        @{user_id="lt_u5"; username="LoadTest User 5"; is_active=$true}
    )
} | ConvertTo-Json -Depth 3

try {
    Invoke-RestMethod -Uri "$BaseUrl/team/add" -Method Post -Body $teamData -ContentType "application/json" -ErrorAction SilentlyContinue | Out-Null
} catch {
    # Team might already exist, ignore
}

Write-Host "Test data created"
Write-Host ""
Write-Host "Starting load test..."
Write-Host "Target: $TotalRequests requests over $TestDuration seconds (~5 RPS)"
Write-Host ""

$startTime = Get-Date
$requestInterval = $TestDuration / $TotalRequests

for ($i = 1; $i -le $TotalRequests; $i++) {
    $iterStart = Get-Date
    
    try {
        # Mix different types of requests
        $reqType = $i % 5
        $reqStart = Get-Date
        
        switch ($reqType) {
            0 {
                # Create PR
                $prData = @{
                    pull_request_id = "loadtest-pr-$i"
                    pull_request_name = "Load Test PR $i"
                    author_id = "lt_u1"
                } | ConvertTo-Json
                
                Invoke-RestMethod -Uri "$BaseUrl/pullRequest/create" -Method Post -Body $prData -ContentType "application/json" -ErrorAction Stop | Out-Null
            }
            1 {
                # Get team
                Invoke-RestMethod -Uri "$BaseUrl/team/get?team_name=loadtest_backend" -Method Get -ErrorAction Stop | Out-Null
            }
            2 {
                # Get stats
                Invoke-RestMethod -Uri "$BaseUrl/stats" -Method Get -ErrorAction Stop | Out-Null
            }
            3 {
                # Get user reviews
                Invoke-RestMethod -Uri "$BaseUrl/users/getReview?user_id=lt_u2" -Method Get -ErrorAction Stop | Out-Null
            }
            4 {
                # Health check
                Invoke-RestMethod -Uri "$BaseUrl/health" -Method Get -ErrorAction Stop | Out-Null
            }
        }
        
        $reqEnd = Get-Date
        $duration = ($reqEnd - $reqStart).TotalMilliseconds
        
        $successCount++
        $responseTimes += $duration
        
        if ($i % 50 -eq 0) {
            Write-Host "Progress: $i/$TotalRequests requests completed"
        }
        
    } catch {
        $failCount++
    }
    
    # Rate limiting
    $iterEnd = Get-Date
    $elapsed = ($iterEnd - $iterStart).TotalSeconds
    $sleepTime = $requestInterval - $elapsed
    
    if ($sleepTime -gt 0) {
        Start-Sleep -Seconds $sleepTime
    }
}

$endTime = Get-Date
$actualDuration = ($endTime - $startTime).TotalSeconds

# Calculate statistics
$totalRequests = $successCount + $failCount
$successRate = if ($totalRequests -gt 0) { ($successCount / $totalRequests) * 100 } else { 0 }
$actualRps = if ($actualDuration -gt 0) { $totalRequests / $actualDuration } else { 0 }

if ($responseTimes.Count -gt 0) {
    $avgResponseTime = ($responseTimes | Measure-Object -Average).Average
    $sortedTimes = $responseTimes | Sort-Object
    
    $p50Idx = [Math]::Floor($sortedTimes.Count * 0.5)
    $p95Idx = [Math]::Floor($sortedTimes.Count * 0.95)
    $p99Idx = [Math]::Floor($sortedTimes.Count * 0.99)
    
    $p50 = $sortedTimes[$p50Idx]
    $p95 = $sortedTimes[$p95Idx]
    $p99 = $sortedTimes[$p99Idx]
} else {
    $avgResponseTime = 0
    $p50 = 0
    $p95 = 0
    $p99 = 0
}

# Print results
Write-Host ""
Write-Host "==============================================" -ForegroundColor Cyan
Write-Host "Load Test Results" -ForegroundColor Cyan
Write-Host "==============================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "Test Duration: $([Math]::Round($actualDuration, 2))s"
Write-Host "Total Requests: $totalRequests"
Write-Host "Successful: $successCount"
Write-Host "Failed: $failCount"
Write-Host ""
Write-Host "Success Rate: $([Math]::Round($successRate, 2))%"
Write-Host "Actual RPS: $([Math]::Round($actualRps, 2))"
Write-Host ""
Write-Host "Response Times (ms):"
Write-Host "  Average: $([Math]::Round($avgResponseTime, 2))ms"
Write-Host "  P50 (median): $([Math]::Round($p50, 2))ms"
Write-Host "  P95: $([Math]::Round($p95, 2))ms"
Write-Host "  P99: $([Math]::Round($p99, 2))ms"
Write-Host ""
Write-Host "Performance Requirements:"

# Check success rate SLI (99.9%)
if ($successRate -ge 99.9) {
    Write-Host "  ✓ Success Rate SLI (99.9%): PASS ($([Math]::Round($successRate, 2))%)" -ForegroundColor Green
} else {
    Write-Host "  ✗ Success Rate SLI (99.9%): FAIL ($([Math]::Round($successRate, 2))%)" -ForegroundColor Red
}

# Check response time SLI (300ms average)
if ($avgResponseTime -le 300) {
    Write-Host "  ✓ Response Time SLI (300ms): PASS ($([Math]::Round($avgResponseTime, 2))ms avg)" -ForegroundColor Green
} else {
    Write-Host "  ! Response Time SLI (300ms): WARNING ($([Math]::Round($avgResponseTime, 2))ms avg)" -ForegroundColor Yellow
}

# Check RPS
if ($actualRps -ge 5) {
    Write-Host "  ✓ RPS Target (5): PASS ($([Math]::Round($actualRps, 2)) RPS)" -ForegroundColor Green
} else {
    Write-Host "  ✗ RPS Target (5): INFO ($([Math]::Round($actualRps, 2)) RPS)" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "==============================================" -ForegroundColor Cyan
