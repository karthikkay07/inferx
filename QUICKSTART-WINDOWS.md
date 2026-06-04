# InferX Quick Start - Windows PowerShell

This guide is specifically for Windows users using PowerShell to test all InferX components.

## 🚀 Quick Setup (5 minutes)

### 1. Prerequisites
Ensure you have:
- **Docker Desktop** installed and running
- **Go 1.22+** installed and in PATH
- **PowerShell** (built into Windows)

### 2. One-Command Setup
```powershell
# Start all services (use docker-compose on Windows)
docker-compose up -d
```

### 3. Verify Everything is Running
```powershell
# Check all services are healthy (PowerShell)
curl http://localhost:8081/health  # Orchestrator
curl http://localhost:8082/health  # Router  
curl http://localhost:8083/health  # Collector
```

Expected response for each: `{"status":"ok"}`

## 🧪 Run Tests

### Quick Test (2 minutes)
```powershell
# Run unit tests
go test ./internal/...

# Test operator locally (no Kubernetes needed)
.\scripts\test-operator-local.ps1
```

### Individual Component Tests

**Router Component:**
```powershell
# Test workload classification
$body = @{
    prompt_tokens = 512
    output_tokens = 256 
    concurrency = 32
    tool_calls = $true
    gpu_profile = "a100-80gb"
    tenant_id = "test"
} | ConvertTo-Json

Invoke-RestMethod -Uri "http://localhost:8082/v1/route" -Method Post -Body $body -ContentType "application/json"

# Expected: {"workload_type": "agent", "recommended_engine": "sglang", ...}
```

**Drift Detector (Collector):**
```powershell
# Insert test metrics data
$insertSql = @"
INSERT INTO metrics.bench_results 
(ts, job_id, engine, model, ttft_p50_ms, ttft_p99_ms, itl_ms, tok_per_s, gpu_mem_mb, kv_cache_hit, error_rate, cost_per_mtok, config)
VALUES 
(NOW(), 'test-1', 'vllm', 'test-model', 45.0, 120.0, 15.0, 85.0, 12000, 0.8, 0.01, 0.05, '{}'),
(NOW(), 'test-2', 'vllm', 'test-model', 65.0, 160.0, 25.0, 65.0, 16000, 0.4, 0.05, 0.09, '{}');
"@

docker-compose exec -T postgres psql -U inferx -d inferx -c $insertSql

# Query metrics
Invoke-RestMethod -Uri "http://localhost:8083/v1/metrics?engine=vllm&model=test-model&since=2024-01-01T00:00:00Z"

# Check for drift detection (wait ~30 seconds)
docker-compose logs collector | Select-String "drift"
```

## 🔄 Component Testing Details

### Router Service (Port 8082)

**Test All Classification Types:**
```powershell
# Function to test classification
function Test-Classification($workload) {
    $body = $workload | ConvertTo-Json
    Invoke-RestMethod -Uri "http://localhost:8082/v1/route" -Method Post -Body $body -ContentType "application/json"
}

# Chat workload
$chatWorkload = @{
    prompt_tokens = 100
    output_tokens = 50
    concurrency = 10
    gpu_profile = "a100-80gb"
    tenant_id = "test"
}
Test-Classification $chatWorkload

# Agent workload (tool calls)  
$agentWorkload = @{
    tool_calls = $true
    gpu_profile = "a100-80gb"
    tenant_id = "test"
}
Test-Classification $agentWorkload

# RAG workload (high prefix sharing)
$ragWorkload = @{
    shared_prefix_ratio = 0.5
    gpu_profile = "a100-80gb"
    tenant_id = "test"
}
Test-Classification $ragWorkload

# Batch workload (long sequences)
$batchWorkload = @{
    prompt_tokens = 2000
    output_tokens = 800
    gpu_profile = "a100-80gb"
    tenant_id = "test"
}
Test-Classification $batchWorkload
```

### Drift Detector (Port 8083)

**Test Baseline Management:**
```powershell
# Get baseline (may return 404 if not set)
Invoke-RestMethod -Uri "http://localhost:8083/v1/baselines?engine=vllm&model=test-model"

# Reset baseline
$resetBody = @{
    engine = "vllm"
    model = "test-model"
} | ConvertTo-Json

Invoke-RestMethod -Uri "http://localhost:8083/v1/baselines/reset" -Method Post -Body $resetBody -ContentType "application/json"

# Query recent metrics
Invoke-RestMethod -Uri "http://localhost:8083/v1/metrics?engine=vllm&model=test-model&since=2024-01-01T00:00:00Z"
```

### Kubernetes Operator

**Option 1: Test Locally (Recommended for Windows)**
```powershell
# Test operator without Kubernetes cluster
.\scripts\test-operator-local.ps1

# This tests:
# - Code compilation
# - Docker image build  
# - CRD validation
# - Configuration
# - Unit tests
```

**Option 2: Use Docker Desktop Kubernetes**
```powershell
# Enable Kubernetes in Docker Desktop Settings first, then:
.\scripts\setup-k8s-test.ps1 check   # Check current setup
.\scripts\setup-k8s-test.ps1 setup   # Setup operator
.\scripts\setup-k8s-test.ps1 test    # Test with sample resource
```

**Option 3: Manual Kubernetes Testing**
```powershell
# If you have a Kubernetes cluster already:
kubectl apply -f k8s\crds\optimized_inference_crd.yaml --validate=false
kubectl apply -f k8s\examples\example_optimized_inference.yaml --validate=false
kubectl get optimizedinferences
kubectl describe optimizedinference llama3-production
```

## 🔍 Troubleshooting

### Services Won't Start
```powershell
# Check Docker is running
docker info

# Check ports are available
Get-NetTCPConnection -LocalPort 5432,8081,8082,8083 -ErrorAction SilentlyContinue

# Reset everything
docker-compose down -v
docker-compose up -d
```

### PowerShell Execution Policy
```powershell
# If scripts won't run, check execution policy
Get-ExecutionPolicy

# If Restricted, allow for current user
Set-ExecutionPolicy -ExecutionPolicy RemoteSigned -Scope CurrentUser
```

### Tests Failing
```powershell
# Check service logs
docker-compose logs

# Check specific service
docker-compose logs router
docker-compose logs collector

# Check database connection
docker-compose exec postgres psql -U inferx -d inferx -c "SELECT 1;"
```

### Kubernetes Issues

**Error: "kubectl: command not found"**
```powershell
# Install kubectl for Windows
choco install kubernetes-cli
# OR download from https://kubernetes.io/docs/tasks/tools/install-kubectl-windows/
```

**Error: "No connection could be made"**
```powershell
# Option 1: Use local testing (no cluster needed)
.\scripts\test-operator-local.ps1

# Option 2: Enable Docker Desktop Kubernetes
# Docker Desktop > Settings > Kubernetes > Enable Kubernetes

# Option 3: Check cluster connection
kubectl cluster-info
```

## 📊 Load Testing

### PowerShell Load Test
```powershell
# Simple load test function
function Start-LoadTest {
    param($Url, $Body, $Requests = 100)
    
    $jobs = @()
    1..$Requests | ForEach-Object {
        $jobs += Start-Job -ScriptBlock {
            param($u, $b)
            Invoke-RestMethod -Uri $u -Method Post -Body $b -ContentType "application/json"
        } -ArgumentList $Url, $Body
    }
    
    $jobs | Wait-Job | Receive-Job
    $jobs | Remove-Job
}

# Test router performance
$testBody = @{
    prompt_tokens = 256
    gpu_profile = "a100-80gb"
    tenant_id = "load-test"
} | ConvertTo-Json

Measure-Command { 
    Start-LoadTest -Url "http://localhost:8082/v1/route" -Body $testBody -Requests 50
}
```

## 🎯 Key Test Scenarios

### 1. End-to-End Workflow
```powershell
# 1. Classify workload
$classificationBody = @{
    prompt_tokens = 512
    gpu_profile = "a100-80gb"
    tenant_id = "e2e-test"
} | ConvertTo-Json

$result = Invoke-RestMethod -Uri "http://localhost:8082/v1/route" -Method Post -Body $classificationBody -ContentType "application/json"
Write-Output "Classification: $($result | ConvertTo-Json)"

# 2. Insert metrics (simulate benchmark results)
$insertSql = "INSERT INTO metrics.bench_results (ts, job_id, engine, model, ttft_p50_ms, ttft_p99_ms, itl_ms, tok_per_s, gpu_mem_mb, kv_cache_hit, error_rate, cost_per_mtok, config) VALUES (NOW(), 'e2e-test', 'vllm', 'test-model', 45.0, 120.0, 15.0, 85.0, 12000, 0.8, 0.01, 0.05, '{}');"
docker-compose exec -T postgres psql -U inferx -d inferx -c $insertSql

# 3. Query metrics
$since = (Get-Date).AddHours(-1).ToString("yyyy-MM-ddTHH:mm:ssZ")
$metrics = Invoke-RestMethod -Uri "http://localhost:8083/v1/metrics?engine=vllm&model=test-model&since=$since"
Write-Output "Metrics: $($metrics | ConvertTo-Json)"

# 4. Check drift detection
Start-Sleep 30
docker-compose logs collector | Select-String "drift"
```

### 2. Error Handling
```powershell
# Test invalid requests (should return errors)
try {
    Invoke-RestMethod -Uri "http://localhost:8082/v1/route" -Method Post -Body '{"invalid":"request"}' -ContentType "application/json"
} catch {
    Write-Output "Expected error: $($_.Exception.Message)"
}
```

## 🧹 Cleanup

```powershell
# Stop all services
docker-compose down

# Clean up completely (removes volumes)
docker-compose down -v

# Clean Kubernetes test environment
.\scripts\setup-k8s-test.ps1 clean
```

## 📝 Summary

You've successfully tested:

✅ **Router Service**: Workload classification with 4 types  
✅ **Drift Detector**: Performance monitoring and alerting  
✅ **Kubernetes Operator**: CRD validation and local testing  
✅ **Integration**: End-to-end workflows  
✅ **Performance**: Load testing with PowerShell

## 🆘 Need Help?

- Check logs: `docker-compose logs`
- Test operator locally: `.\scripts\test-operator-local.ps1`
- Check Kubernetes: `.\scripts\setup-k8s-test.ps1 check`
- Reset environment: `docker-compose down -v; docker-compose up -d`

## 🔧 Windows-Specific Commands

**Build binaries:**
```powershell
go build -o bin\router.exe .\cmd\router
go build -o bin\collector.exe .\cmd\collector
go build -o bin\operator.exe .\cmd\operator
```

**Run services manually:**
```powershell
$env:DATABASE_URL="postgres://inferx:inferx@localhost:5432/inferx"
$env:ORCHESTRATOR_URL="http://localhost:8081"
$env:PORT="8082"
.\bin\router.exe
```

**PowerShell Docker shortcuts:**
```powershell
# Restart single service
docker-compose restart router

# View logs with timestamps
docker-compose logs -t -f collector

# Execute commands in containers
docker-compose exec postgres psql -U inferx -d inferx
```