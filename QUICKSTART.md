# InferX Quick Start Testing Guide

This guide will help you quickly test all the InferX components we've built.

> **Windows Users**: See [QUICKSTART-WINDOWS.md](./QUICKSTART-WINDOWS.md) for PowerShell-specific instructions.

## 🚀 Quick Setup (5 minutes)

### 1. Prerequisites
Ensure you have:
- **Docker** and **Docker Compose** installed
- **Go 1.22+** installed
- **curl** installed
- **make** available

### 2. One-Command Setup
```bash
# Setup test environment
chmod +x scripts/setup-test-env.sh
./scripts/setup-test-env.sh

# Start all services
make start
```

### 3. Verify Everything is Running
```bash
# Check all services are healthy
make test-health
```

Expected output:
```
Orchestrator: OK
Router: OK  
Collector: OK
```

## 🧪 Run Tests

### Quick Test (2 minutes)
```bash
# Run all tests
./run-tests.sh
```

### Individual Component Tests

**Unit Tests:**
```bash
make test-unit
```

**Router Component:**
```bash
# Test workload classification
curl -X POST http://localhost:8082/v1/route \
  -H "Content-Type: application/json" \
  -d '{
    "prompt_tokens": 512,
    "output_tokens": 256, 
    "concurrency": 32,
    "tool_calls": true,
    "gpu_profile": "a100-80gb",
    "tenant_id": "test"
  }'

# Expected: {"workload_type": "agent", "recommended_engine": "sglang", ...}
```

**Drift Detector (Collector):**
```bash
# Insert test metrics data
docker-compose exec -T postgres psql -U inferx -d inferx << EOF
INSERT INTO metrics.bench_results 
(ts, job_id, engine, model, ttft_p50_ms, ttft_p99_ms, itl_ms, tok_per_s, gpu_mem_mb, kv_cache_hit, error_rate, cost_per_mtok, config)
VALUES 
(NOW(), 'test-1', 'vllm', 'test-model', 45.0, 120.0, 15.0, 85.0, 12000, 0.8, 0.01, 0.05, '{}'),
(NOW(), 'test-2', 'vllm', 'test-model', 65.0, 160.0, 25.0, 65.0, 16000, 0.4, 0.05, 0.09, '{}');
EOF

# Query metrics
curl "http://localhost:8083/v1/metrics?engine=vllm&model=test-model&since=2024-01-01T00:00:00Z"

# Check for drift detection (wait ~30 seconds for detector to run)
docker-compose logs collector | grep -i "drift"
```

## 🔄 Component Testing Details

### Router Service (Port 8082)

**Test Classification Logic:**
```bash
# Chat workload
curl -X POST http://localhost:8082/v1/route \
  -d '{"prompt_tokens":100,"output_tokens":50,"concurrency":10,"gpu_profile":"a100-80gb","tenant_id":"test"}'

# Agent workload (tool calls)  
curl -X POST http://localhost:8082/v1/route \
  -d '{"tool_calls":true,"gpu_profile":"a100-80gb","tenant_id":"test"}'

# RAG workload (high prefix sharing)
curl -X POST http://localhost:8082/v1/route \
  -d '{"shared_prefix_ratio":0.5,"gpu_profile":"a100-80gb","tenant_id":"test"}'

# Batch workload (long sequences)
curl -X POST http://localhost:8082/v1/route \
  -d '{"prompt_tokens":2000,"output_tokens":800,"gpu_profile":"a100-80gb","tenant_id":"test"}'
```

### Drift Detector (Port 8083)

**Test Baseline Management:**
```bash
# Get baseline (may return 404 if not set)
curl "http://localhost:8083/v1/baselines?engine=vllm&model=test-model"

# Reset baseline
curl -X POST http://localhost:8083/v1/baselines/reset \
  -H "Content-Type: application/json" \
  -d '{"engine":"vllm","model":"test-model"}'

# Query recent metrics
curl "http://localhost:8083/v1/metrics?engine=vllm&model=test-model&since=2024-01-01T00:00:00Z"
```

### Kubernetes Operator

**Option 1: Test without Kubernetes cluster (Local testing):**
```bash
# Test operator logic, build, and configuration locally
chmod +x scripts/test-operator-local.sh
./scripts/test-operator-local.sh
```

**Option 2: Setup local cluster and test:**
```bash
# Automated setup (installs k3d/kind if needed)
chmod +x scripts/setup-k8s-test.sh
./scripts/setup-k8s-test.sh setup

# Test with sample resource
./scripts/setup-k8s-test.sh test
```

**Option 3: Use existing cluster:**
```bash
# Apply CRDs (skip validation if cluster is new)
kubectl apply -f k8s/crds/optimized_inference_crd.yaml --validate=false

# Deploy operator (update namespace as needed)
kubectl create namespace inferx-system
kubectl apply -f k8s/helm/templates/operator-deployment.yaml -n inferx-system

# Test with sample resource
kubectl apply -f k8s/examples/example_optimized_inference.yaml --validate=false

# Check status
kubectl get optimizedinferences
kubectl describe optimizedinference llama3-production
```

## 🔍 Troubleshooting

### Services Won't Start
```bash
# Check Docker daemon
docker info

# Check ports
netstat -tulpn | grep -E ':(5432|8081|8082|8083)'

# Reset everything
make clean
make start
```

### Tests Failing
```bash
# Check service logs
make logs

# Check database connection
docker-compose exec postgres psql -U inferx -d inferx -c "SELECT 1;"

# Restart services
make restart
```

### Database Issues
```bash
# Reset database
make clean
docker-compose up -d postgres
sleep 10

# Verify tables exist
docker-compose exec postgres psql -U inferx -d inferx -c "\dt public.*; \dt metrics.*;"
```

### Kubernetes Issues

**Error: "dial tcp [::1]:8080: connectex: No connection could be made"**
This means kubectl can't connect to a Kubernetes cluster. Solutions:

```bash
# Option 1: Test operator locally (no cluster needed)
./scripts/test-operator-local.sh

# Option 2: Setup local cluster automatically
./scripts/setup-k8s-test.sh setup

# Option 3: Use Docker Desktop Kubernetes
# Enable in Docker Desktop Settings > Kubernetes > Enable Kubernetes

# Option 4: Manual cluster setup
# Install k3d: curl -s https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash
k3d cluster create inferx-test --agents 1

# Then apply resources with --validate=false
kubectl apply -f k8s/crds/optimized_inference_crd.yaml --validate=false
```

## 📊 Load Testing

### Quick Load Test
```bash
# Install hey (if not already installed)
go install github.com/rakyll/hey@latest

# Router load test
hey -n 100 -c 10 -m POST \
  -H "Content-Type: application/json" \
  -d '{"prompt_tokens":256,"gpu_profile":"a100-80gb","tenant_id":"load-test"}' \
  http://localhost:8082/v1/route
```

### Full Load Test Suite
```bash
./scripts/test-load.sh
```

## 🎯 Key Test Scenarios

### 1. End-to-End Workflow
```bash
# 1. Classify workload (Router)
RESULT=$(curl -s -X POST http://localhost:8082/v1/route -d '{"prompt_tokens":512,"gpu_profile":"a100-80gb","tenant_id":"e2e-test"}')
echo "Classification: $RESULT"

# 2. Insert metrics (simulate benchmark results)
docker-compose exec -T postgres psql -U inferx -d inferx -c "
INSERT INTO metrics.bench_results (ts, job_id, engine, model, ttft_p50_ms, ttft_p99_ms, itl_ms, tok_per_s, gpu_mem_mb, kv_cache_hit, error_rate, cost_per_mtok, config)
VALUES (NOW(), 'e2e-test', 'vllm', 'test-model', 45.0, 120.0, 15.0, 85.0, 12000, 0.8, 0.01, 0.05, '{}');"

# 3. Query metrics (Collector)
curl "http://localhost:8083/v1/metrics?engine=vllm&model=test-model&since=$(date -u -d '1 hour ago' '+%Y-%m-%dT%H:%M:%SZ')"

# 4. Check drift detection (wait 30s then check logs)
sleep 30
docker-compose logs collector | tail -20
```

### 2. Error Handling
```bash
# Test invalid requests
curl -X POST http://localhost:8082/v1/route -d '{"invalid":"request"}'
curl "http://localhost:8083/v1/metrics?engine=missing_param"
```

### 3. Performance Validation
```bash
# Response time should be < 100ms for router
time curl -s http://localhost:8082/v1/route -d '{"prompt_tokens":100,"gpu_profile":"a100-80gb","tenant_id":"perf-test"}' > /dev/null

# Database queries should be < 500ms
time curl -s "http://localhost:8083/v1/metrics?engine=vllm&model=test&since=2024-01-01T00:00:00Z" > /dev/null
```

## 🧹 Cleanup

```bash
# Stop all services
make stop

# Clean up completely (removes volumes)
make clean

# Remove Kubernetes cluster (if created)
k3d cluster delete inferx-test
```

## 📝 Summary

You've successfully tested:

✅ **Router Service**: Workload classification with 4 types (chat, batch, rag, agent)  
✅ **Drift Detector**: Performance monitoring with baseline management and alerting  
✅ **Kubernetes Operator**: Auto-optimization with CRD management  
✅ **Integration**: End-to-end workflows between components  
✅ **Performance**: Load testing and response time validation

For detailed testing information, see [TESTING.md](./TESTING.md).

## 🆘 Need Help?

- Check logs: `make logs`
- View all commands: `make help`
- Run health checks: `make test-health`
- Reset environment: `make clean && make start`