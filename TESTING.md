# InferX Testing Guide

This guide covers how to test all components of the InferX system, including unit tests, integration tests, and end-to-end testing.

## Prerequisites

1. **Go 1.22+** installed
2. **Docker** and **Docker Compose** installed
3. **kubectl** (for Kubernetes operator testing)
4. **k3d** or **kind** (for local Kubernetes testing)

## Quick Start

```bash
# 1. Run unit tests
go test ./...

# 2. Start infrastructure
docker-compose up -d

# 3. Run integration tests
make test-integration

# 4. Test individual components
make test-router
make test-collector
make test-operator
```

## 1. Unit Testing

### Run All Unit Tests
```bash
go test ./...
go test -v ./internal/router/
go test -v ./internal/drift/
```

### Coverage Report
```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### Specific Component Tests

**Router Tests:**
```bash
go test -v ./internal/router/ -run TestClassify
```

**Drift Detector Tests:**
```bash
go test -v ./internal/drift/ -run TestComputeFromResults
go test -v ./internal/drift/ -run TestCheckMetric
```

## 2. Component Testing

### Router Service

#### Start Dependencies
```bash
# Start PostgreSQL and orchestrator
docker-compose up -d postgres orchestrator
```

#### Build and Run Router
```bash
go build -o bin/router ./cmd/router
export DATABASE_URL="postgres://inferx:inferx@localhost:5432/inferx"
export ORCHESTRATOR_URL="http://localhost:8081"
export PORT="8082"
./bin/router
```

#### Test Router API
```bash
# Test classification endpoint
curl -X POST http://localhost:8082/v1/route \
  -H "Content-Type: application/json" \
  -d '{
    "prompt_tokens": 512,
    "output_tokens": 256,
    "concurrency": 32,
    "structured_output": false,
    "tool_calls": false,
    "shared_prefix_ratio": 0.2,
    "gpu_profile": "a100-80gb",
    "tenant_id": "test-tenant"
  }'

# Expected response:
# {
#   "workload_type": "batch",
#   "recommended_engine": "vllm",
#   "engine": "vllm",
#   "worker_id": "worker-123",
#   "worker_url": "http://worker-123:8000",
#   "used_fallback": false
# }

# Test health endpoint
curl http://localhost:8082/health
```

### Drift Detector (Collector)

#### Build and Run Collector
```bash
go build -o bin/collector ./cmd/collector
export DATABASE_URL="postgres://inferx:inferx@localhost:5432/inferx"
export PORT="8083"
export SLACK_WEBHOOK_URL=""  # Optional
export DRIFT_CHECK_INTERVAL="1m"
export DRIFT_LOOKBACK="1h"
./bin/collector
```

#### Test Collector API
```bash
# Get metrics
curl "http://localhost:8083/v1/metrics?engine=vllm&model=llama-7b&since=2024-01-01T00:00:00Z"

# Get baseline
curl "http://localhost:8083/v1/baselines?engine=vllm&model=llama-7b"

# Reset baseline
curl -X POST http://localhost:8083/v1/baselines/reset \
  -H "Content-Type: application/json" \
  -d '{"engine": "vllm", "model": "llama-7b"}'

# Health check
curl http://localhost:8083/health
```

#### Generate Test Data
```bash
# Insert sample metrics to trigger drift detection
psql postgres://inferx:inferx@localhost:5432/inferx << EOF
INSERT INTO metrics.bench_results 
(ts, job_id, engine, model, ttft_p50_ms, ttft_p99_ms, itl_ms, tok_per_s, gpu_mem_mb, kv_cache_hit, error_rate, cost_per_mtok, config)
VALUES 
(NOW(), 'test-1', 'vllm', 'llama-7b', 45, 120, 15, 85, 12000, 0.8, 0.01, 0.05, '{}'),
(NOW(), 'test-2', 'vllm', 'llama-7b', 55, 140, 18, 75, 14000, 0.7, 0.02, 0.06, '{}');
EOF
```

### Kubernetes Operator

#### Setup Local Kubernetes
```bash
# Using k3d
k3d cluster create inferx --agents 1

# Or using kind
kind create cluster --name inferx
```

#### Install CRD
```bash
kubectl apply -f k8s/crds/optimized_inference_crd.yaml
kubectl get crd optimizedinferences.inferx.io
```

#### Build and Deploy Operator
```bash
# Build operator binary
go build -o bin/operator ./cmd/operator

# Create Docker image (optional)
docker build -t inferx-operator:latest -f Dockerfile.operator .

# Deploy with environment variables
kubectl create secret generic operator-config \
  --from-literal=ORCHESTRATOR_URL="http://orchestrator:8081"

kubectl apply -f k8s/helm/templates/operator-deployment.yaml
```

#### Test Operator
```bash
# Apply test resource
kubectl apply -f k8s/examples/example_optimized_inference.yaml

# Check status
kubectl get optimizedinferences
kubectl describe optimizedinference llama3-production

# Check created deployment
kubectl get deployments
kubectl describe deployment inferx-llama3-production

# Check logs
kubectl logs -l app=inferx-operator
```

## 3. Integration Testing

### End-to-End Workflow Test

Create a comprehensive test script:

```bash
#!/bin/bash
# test-e2e.sh

set -e

echo "=== Starting E2E Test ==="

# 1. Start infrastructure
docker-compose up -d
sleep 30

# 2. Submit benchmark job
JOB_ID=$(curl -s -X POST http://localhost:8081/v1/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "model": "llama-7b",
    "engines": ["vllm", "sglang"],
    "workload": {"concurrency": 32, "prompt_tokens": 512, "output_tokens": 256, "num_requests": 100},
    "gpu_profile": "a100-80gb"
  }' | jq -r '.id')

echo "Submitted job: $JOB_ID"

# 3. Wait for job completion (simulate)
sleep 10

# 4. Test router classification
ROUTE_RESULT=$(curl -s -X POST http://localhost:8082/v1/route \
  -H "Content-Type: application/json" \
  -d '{
    "prompt_tokens": 512,
    "output_tokens": 256,
    "concurrency": 32,
    "gpu_profile": "a100-80gb",
    "tenant_id": "test"
  }')

echo "Route result: $ROUTE_RESULT"

# 5. Check drift detector
curl -s http://localhost:8083/health

# 6. Test Kubernetes operator
kubectl apply -f k8s/examples/example_optimized_inference.yaml
sleep 5
kubectl get optimizedinferences

echo "=== E2E Test Complete ==="
```

### Load Testing

#### Router Load Test
```bash
# Install hey for load testing
go install github.com/rakyll/hey@latest

# Load test router
hey -n 1000 -c 10 -m POST \
  -H "Content-Type: application/json" \
  -d '{"prompt_tokens":256,"output_tokens":128,"concurrency":16,"gpu_profile":"a100-80gb","tenant_id":"load-test"}' \
  http://localhost:8082/v1/route
```

#### Drift Detector Stress Test
```bash
# Generate lots of metrics data
for i in {1..100}; do
  psql postgres://inferx:inferx@localhost:5432/inferx -c \
    "INSERT INTO metrics.bench_results (ts, job_id, engine, model, ttft_p50_ms, ttft_p99_ms, itl_ms, tok_per_s, gpu_mem_mb, kv_cache_hit, error_rate, cost_per_mtok, config) 
     VALUES (NOW(), 'load-$i', 'vllm', 'test-model', $((45 + RANDOM % 20)), $((120 + RANDOM % 40)), 15, $((80 + RANDOM % 20)), 12000, 0.8, 0.01, 0.05, '{}');"
done
```

## 4. Database Testing

### Migration Testing
```bash
# Test migrations
docker-compose up -d postgres
sleep 10

# Verify tables exist
psql postgres://inferx:inferx@localhost:5432/inferx -c "\dt public.*"
psql postgres://inferx:inferx@localhost:5432/inferx -c "\dt metrics.*"

# Verify baseline table
psql postgres://inferx:inferx@localhost:5432/inferx -c "\d public.baselines"
```

### Data Validation
```bash
# Test baseline operations
psql postgres://inferx:inferx@localhost:5432/inferx << EOF
-- Insert test baseline
INSERT INTO public.baselines (engine, model, ttft_p50_ms, ttft_p99_ms, tok_per_sec, gpu_mem_mb, sample_count)
VALUES ('vllm', 'test-model', 45.5, 120.0, 85.2, 12000, 50);

-- Verify data
SELECT * FROM public.baselines WHERE engine = 'vllm' AND model = 'test-model';
EOF
```

## 5. Error Testing

### Test Error Conditions

```bash
# Test router with invalid request
curl -X POST http://localhost:8082/v1/route \
  -H "Content-Type: application/json" \
  -d '{"invalid": "json"}'

# Test collector with missing parameters
curl "http://localhost:8083/v1/metrics?engine=vllm"

# Test operator with invalid CRD
kubectl apply -f - << EOF
apiVersion: inferx.io/v1alpha1
kind: OptimizedInference
metadata:
  name: invalid-test
spec:
  model: "test"
  engine: "invalid-engine"  # Invalid enum value
  gpuProfile: "a100-80gb"
  replicas: 1
  resources:
    gpuCount: 1
    memoryGB: 8
    cpuCores: 2
EOF
```

## 6. Monitoring and Debugging

### Logs
```bash
# View router logs
docker-compose logs -f router

# View collector logs
docker-compose logs -f collector

# View operator logs
kubectl logs -l app=inferx-operator -f
```

### Metrics
```bash
# Check database connections
psql postgres://inferx:inferx@localhost:5432/inferx -c "SELECT count(*) FROM pg_stat_activity WHERE datname = 'inferx';"

# Check cache performance (if accessible)
curl http://localhost:8082/debug/cache  # If debug endpoint exists
```

## 7. Cleanup

```bash
# Stop services
docker-compose down

# Clean Kubernetes
kubectl delete optimizedinference --all
kubectl delete -f k8s/helm/templates/operator-deployment.yaml
kubectl delete -f k8s/crds/optimized_inference_crd.yaml

# Remove cluster
k3d cluster delete inferx
# or
kind delete cluster --name inferx
```

## Makefile Targets

Create a Makefile for common operations:

```makefile
.PHONY: test test-unit test-integration build clean

test: test-unit test-integration

test-unit:
	go test -v ./...

test-integration:
	./scripts/test-e2e.sh

build:
	go build -o bin/router ./cmd/router
	go build -o bin/collector ./cmd/collector
	go build -o bin/operator ./cmd/operator

clean:
	rm -rf bin/
	docker-compose down -v

dev:
	docker-compose up -d postgres
	sleep 10
	DATABASE_URL="postgres://inferx:inferx@localhost:5432/inferx" go run ./cmd/router &
	DATABASE_URL="postgres://inferx:inferx@localhost:5432/inferx" go run ./cmd/collector &
```

This comprehensive testing guide covers all aspects of testing the InferX system components.