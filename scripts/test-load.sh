#!/bin/bash
# Load test script for InferX components

set -e

echo "=== InferX Load Tests ==="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if services are running
check_service() {
    local service_name=$1
    local url=$2
    
    if curl -f -s "$url" > /dev/null; then
        log_info "$service_name is running"
        return 0
    else
        log_error "$service_name is not running at $url"
        return 1
    fi
}

# Load test function
run_load_test() {
    local test_name=$1
    local url=$2
    local method=$3
    local data=$4
    local concurrent_users=$5
    local total_requests=$6
    
    log_info "Running load test: $test_name"
    log_info "  URL: $url"
    log_info "  Method: $method"
    log_info "  Concurrent users: $concurrent_users"
    log_info "  Total requests: $total_requests"
    
    if command -v hey > /dev/null; then
        # Use hey for load testing
        if [ "$method" = "POST" ]; then
            hey -n "$total_requests" -c "$concurrent_users" -m POST \
                -H "Content-Type: application/json" \
                -d "$data" \
                "$url"
        else
            hey -n "$total_requests" -c "$concurrent_users" "$url"
        fi
    elif command -v ab > /dev/null; then
        # Use Apache Bench as fallback
        if [ "$method" = "POST" ]; then
            echo "$data" > /tmp/post_data.json
            ab -n "$total_requests" -c "$concurrent_users" \
               -T "application/json" \
               -p /tmp/post_data.json \
               "$url"
        else
            ab -n "$total_requests" -c "$concurrent_users" "$url"
        fi
    else
        log_warn "Neither 'hey' nor 'ab' (Apache Bench) found. Running basic curl test..."
        run_basic_load_test "$url" "$method" "$data" "$total_requests"
    fi
    
    echo ""
}

run_basic_load_test() {
    local url=$1
    local method=$2
    local data=$3
    local requests=$4
    
    log_info "Running basic load test with curl..."
    local start_time=$(date +%s)
    local successful=0
    local failed=0
    
    for i in $(seq 1 "$requests"); do
        if [ "$method" = "POST" ]; then
            if curl -s -f -X POST -H "Content-Type: application/json" -d "$data" "$url" > /dev/null; then
                ((successful++))
            else
                ((failed++))
            fi
        else
            if curl -s -f "$url" > /dev/null; then
                ((successful++))
            else
                ((failed++))
            fi
        fi
        
        # Progress indicator
        if [ $((i % 10)) -eq 0 ]; then
            echo -n "."
        fi
    done
    
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    echo ""
    log_info "Load test completed:"
    log_info "  Duration: ${duration}s"
    log_info "  Successful requests: $successful"
    log_info "  Failed requests: $failed"
    log_info "  Requests per second: $(echo "scale=2; $successful / $duration" | bc -l)"
}

# Install load testing tools if not present
install_tools() {
    if ! command -v hey > /dev/null && ! command -v ab > /dev/null; then
        log_warn "No load testing tools found. Installing 'hey'..."
        
        if command -v go > /dev/null; then
            go install github.com/rakyll/hey@latest
            export PATH=$PATH:$(go env GOPATH)/bin
        else
            log_warn "Go not found. Please install 'hey' or 'ab' manually for better load testing."
            log_warn "  hey: go install github.com/rakyll/hey@latest"
            log_warn "  ab:  apt-get install apache2-utils (Ubuntu/Debian)"
        fi
    fi
}

# Main load testing
main() {
    log_info "Starting load tests for InferX components..."
    
    # Install tools if needed
    install_tools
    
    # Check if services are running
    check_service "Orchestrator" "http://localhost:8081/health" || exit 1
    check_service "Router" "http://localhost:8082/health" || exit 1
    check_service "Collector" "http://localhost:8083/health" || exit 1
    
    # Test 1: Router Classification Load Test
    log_info "=== Router Load Test ==="
    
    # Light load test
    run_load_test "Router - Light Load" \
        "http://localhost:8082/v1/route" \
        "POST" \
        '{"prompt_tokens":256,"output_tokens":128,"concurrency":16,"gpu_profile":"a100-80gb","tenant_id":"load-test"}' \
        5 \
        50
    
    # Medium load test
    run_load_test "Router - Medium Load" \
        "http://localhost:8082/v1/route" \
        "POST" \
        '{"prompt_tokens":512,"output_tokens":256,"concurrency":32,"gpu_profile":"a100-80gb","tenant_id":"load-test"}' \
        10 \
        100
    
    # Heavy load test
    run_load_test "Router - Heavy Load" \
        "http://localhost:8082/v1/route" \
        "POST" \
        '{"prompt_tokens":1024,"output_tokens":512,"concurrency":64,"gpu_profile":"h100","tenant_id":"load-test"}' \
        20 \
        200
    
    # Test 2: Collector Load Test
    log_info "=== Collector Load Test ==="
    
    # First, insert test data for metrics queries
    log_info "Inserting test data for metrics queries..."
    docker-compose exec -T postgres psql -U inferx -d inferx << EOF > /dev/null
-- Insert test metrics data
INSERT INTO metrics.bench_results 
(ts, job_id, engine, model, ttft_p50_ms, ttft_p99_ms, itl_ms, tok_per_s, gpu_mem_mb, kv_cache_hit, error_rate, cost_per_mtok, config)
SELECT 
    NOW() - (random() * INTERVAL '24 hours'),
    'load-test-' || generate_series,
    CASE (random() * 3)::int 
        WHEN 0 THEN 'vllm'
        WHEN 1 THEN 'sglang' 
        ELSE 'tensorrt'
    END,
    'load-test-model-' || ((random() * 5)::int + 1),
    45 + (random() * 20),
    120 + (random() * 40),
    15 + (random() * 10),
    80 + (random() * 20),
    12000 + (random() * 4000)::int,
    0.5 + (random() * 0.4),
    random() * 0.05,
    0.05 + (random() * 0.03),
    '{}'
FROM generate_series(1, 1000);
EOF
    
    # Health check load test
    run_load_test "Collector - Health Check" \
        "http://localhost:8083/health" \
        "GET" \
        "" \
        10 \
        100
    
    # Metrics query load test
    run_load_test "Collector - Metrics Query" \
        "http://localhost:8083/v1/metrics?engine=vllm&model=load-test-model-1&since=2024-01-01T00:00:00Z" \
        "GET" \
        "" \
        5 \
        50
    
    # Baseline query load test
    run_load_test "Collector - Baseline Query" \
        "http://localhost:8083/v1/baselines?engine=vllm&model=load-test-model-1" \
        "GET" \
        "" \
        5 \
        25
    
    # Test 3: Mixed Workload Test
    log_info "=== Mixed Workload Test ==="
    
    # Simulate realistic mixed workload
    log_info "Running mixed workload simulation..."
    
    mixed_workload() {
        local iterations=$1
        
        for i in $(seq 1 "$iterations"); do
            # Router classification (70% of traffic)
            if [ $((RANDOM % 10)) -lt 7 ]; then
                curl -s -f -X POST http://localhost:8082/v1/route \
                    -H "Content-Type: application/json" \
                    -d "{
                        \"prompt_tokens\": $((200 + RANDOM % 800)),
                        \"output_tokens\": $((100 + RANDOM % 400)),
                        \"concurrency\": $((10 + RANDOM % 50)),
                        \"gpu_profile\": \"a100-80gb\",
                        \"tenant_id\": \"mixed-test-$((RANDOM % 10))\"
                    }" > /dev/null &
            
            # Metrics queries (20% of traffic)
            elif [ $((RANDOM % 10)) -lt 9 ]; then
                local model_num=$((RANDOM % 5 + 1))
                curl -s -f "http://localhost:8083/v1/metrics?engine=vllm&model=load-test-model-$model_num&since=2024-01-01T00:00:00Z" > /dev/null &
            
            # Baseline operations (10% of traffic)
            else
                local model_num=$((RANDOM % 5 + 1))
                curl -s -f "http://localhost:8083/v1/baselines?engine=vllm&model=load-test-model-$model_num" > /dev/null &
            fi
            
            # Control rate
            if [ $((i % 20)) -eq 0 ]; then
                wait  # Wait for background jobs to complete
                echo -n "."
            fi
        done
        
        wait  # Wait for all background jobs to complete
    }
    
    local start_time=$(date +%s)
    mixed_workload 200
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    echo ""
    log_info "Mixed workload completed in ${duration}s"
    
    # Test 4: Stress Test
    log_info "=== Stress Test ==="
    
    log_warn "Running stress test - this may impact system performance..."
    
    # High concurrency router test
    run_load_test "Router - Stress Test" \
        "http://localhost:8082/v1/route" \
        "POST" \
        '{"prompt_tokens":100,"output_tokens":50,"concurrency":10,"gpu_profile":"a100-80gb","tenant_id":"stress-test"}' \
        50 \
        500
    
    # Test 5: Database Load Test
    log_info "=== Database Load Test ==="
    
    log_info "Testing database performance with metrics insertion..."
    
    # Insert large batch of metrics
    local db_start_time=$(date +%s)
    docker-compose exec -T postgres psql -U inferx -d inferx << EOF > /dev/null
INSERT INTO metrics.bench_results 
(ts, job_id, engine, model, ttft_p50_ms, ttft_p99_ms, itl_ms, tok_per_s, gpu_mem_mb, kv_cache_hit, error_rate, cost_per_mtok, config)
SELECT 
    NOW() - (random() * INTERVAL '1 hour'),
    'db-load-test-' || generate_series,
    'vllm',
    'db-test-model',
    45 + (random() * 20),
    120 + (random() * 40),
    15 + (random() * 10),
    80 + (random() * 20),
    12000,
    0.8,
    0.01,
    0.05,
    '{}'
FROM generate_series(1, 5000);
EOF
    local db_end_time=$(date +%s)
    local db_duration=$((db_end_time - db_start_time))
    
    log_info "Database load test: Inserted 5000 records in ${db_duration}s"
    
    # Test query performance
    local query_start_time=$(date +%s)
    docker-compose exec -T postgres psql -U inferx -d inferx -c "
        SELECT engine, model, AVG(ttft_p50_ms), AVG(tok_per_s) 
        FROM metrics.bench_results 
        WHERE job_id LIKE 'db-load-test-%' 
        GROUP BY engine, model;
    " > /dev/null
    local query_end_time=$(date +%s)
    local query_duration=$((query_end_time - query_start_time))
    
    log_info "Database query test: Aggregated 5000 records in ${query_duration}s"
    
    # Summary
    log_info "=== Load Test Summary ==="
    log_info "✅ Router classification under various loads"
    log_info "✅ Collector metrics and baseline queries"
    log_info "✅ Mixed workload simulation"
    log_info "✅ Stress testing with high concurrency"
    log_info "✅ Database performance testing"
    
    echo -e "${GREEN}🎉 All load tests completed!${NC}"
    
    # Performance recommendations
    log_info "=== Performance Recommendations ==="
    log_warn "Monitor the following metrics in production:"
    log_warn "  - Router response times should be < 100ms"
    log_warn "  - Database query times should be < 500ms"
    log_warn "  - Memory usage should remain stable under load"
    log_warn "  - CPU usage should not exceed 80% sustained"
}

# Run main function
main "$@"