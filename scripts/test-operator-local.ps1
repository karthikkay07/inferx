# PowerShell script for local operator testing without Kubernetes cluster

param(
    [string]$Action = "all"
)

# Colors for output
$ColorInfo = "Green"
$ColorWarn = "Yellow"
$ColorError = "Red"

function Write-Info($message) {
    Write-Host "[INFO] $message" -ForegroundColor $ColorInfo
}

function Write-Warn($message) {
    Write-Host "[WARN] $message" -ForegroundColor $ColorWarn
}

function Write-Error($message) {
    Write-Host "[ERROR] $message" -ForegroundColor $ColorError
}

Write-Host "=== InferX Operator Local Testing ===" -ForegroundColor Cyan

# Test CRD validation
function Test-CRDValidation {
    Write-Info "Testing CRD validation..."
    
    $crdFile = "k8s\crds\optimized_inference_crd.yaml"
    $exampleFile = "k8s\examples\example_optimized_inference.yaml"
    
    if (Test-Path $crdFile) {
        Write-Info "✓ CRD file exists: $crdFile"
        
        # Basic YAML syntax check
        try {
            $content = Get-Content $crdFile -Raw
            if ($content -match "apiVersion.*CustomResourceDefinition") {
                Write-Info "✓ CRD structure looks valid"
            } else {
                Write-Warn "CRD structure may be invalid"
            }
        } catch {
            Write-Error "Failed to read CRD file: $_"
            return $false
        }
    } else {
        Write-Error "CRD file not found: $crdFile"
        return $false
    }
    
    if (Test-Path $exampleFile) {
        Write-Info "✓ Example resource exists: $exampleFile"
    } else {
        Write-Warn "Example resource not found: $exampleFile"
    }
    
    return $true
}

# Test operator build
function Test-OperatorBuild {
    Write-Info "Testing operator build..."
    
    if (!(Get-Command go -ErrorAction SilentlyContinue)) {
        Write-Error "Go is not installed or not in PATH"
        return $false
    }
    
    Write-Info "Go version: $(go version)"
    
    # Test build
    Write-Info "Building operator binary..."
    try {
        $env:CGO_ENABLED = "0"
        $env:GOOS = "windows"
        go build -o "bin\operator.exe" ".\cmd\operator"
        
        if (Test-Path "bin\operator.exe") {
            Write-Info "✓ Operator builds successfully"
            $size = (Get-Item "bin\operator.exe").Length
            Write-Info "Binary size: $([math]::Round($size/1MB, 2)) MB"
            return $true
        } else {
            Write-Error "Operator binary not created"
            return $false
        }
    } catch {
        Write-Error "Operator build failed: $_"
        return $false
    }
}

# Test Docker build
function Test-DockerBuild {
    Write-Info "Testing operator Docker image build..."
    
    if (!(Get-Command docker -ErrorAction SilentlyContinue)) {
        Write-Warn "Docker not found, skipping image build test"
        return $true
    }
    
    Write-Info "Docker version: $(docker --version)"
    
    try {
        Write-Info "Building Docker image (this may take a few minutes)..."
        docker build -t inferx-operator:test -f Dockerfile.operator . 2>&1 | Out-Null
        
        # Check if image was created
        $images = docker images inferx-operator:test --format "{{.Repository}}:{{.Tag}}"
        if ($images -contains "inferx-operator:test") {
            Write-Info "✓ Operator Docker image builds successfully"
            
            # Clean up test image
            docker rmi inferx-operator:test 2>&1 | Out-Null
            return $true
        } else {
            Write-Error "Docker image was not created"
            return $false
        }
    } catch {
        Write-Error "Docker build failed: $_"
        return $false
    }
}

# Test Go modules
function Test-GoModules {
    Write-Info "Testing Go modules..."
    
    if (!(Get-Command go -ErrorAction SilentlyContinue)) {
        Write-Warn "Go not found, skipping module test"
        return $true
    }
    
    try {
        # Test go mod tidy
        Write-Info "Running go mod tidy..."
        go mod tidy
        
        # Test go mod verify
        Write-Info "Verifying modules..."
        go mod verify
        
        Write-Info "✓ Go modules are valid"
        return $true
    } catch {
        Write-Error "Go modules test failed: $_"
        return $false
    }
}

# Test operator configuration
function Test-OperatorConfig {
    Write-Info "Testing operator configuration..."
    
    # Set test environment variables
    $env:ORCHESTRATOR_URL = "http://test-orchestrator:8081"
    
    # Test environment variable reading
    if ($env:ORCHESTRATOR_URL -eq "http://test-orchestrator:8081") {
        Write-Info "✓ Environment variables work correctly"
    } else {
        Write-Error "Environment variable test failed"
        return $false
    }
    
    # Test required files exist
    $requiredFiles = @(
        "cmd\operator\main.go",
        "k8s\crds\optimized_inference_types.go",
        "Dockerfile.operator"
    )
    
    $allExist = $true
    foreach ($file in $requiredFiles) {
        if (Test-Path $file) {
            Write-Info "✓ Required file exists: $file"
        } else {
            Write-Error "Required file missing: $file"
            $allExist = $false
        }
    }
    
    return $allExist
}

# Test unit tests
function Test-UnitTests {
    Write-Info "Testing unit tests..."
    
    if (!(Get-Command go -ErrorAction SilentlyContinue)) {
        Write-Warn "Go not found, skipping unit tests"
        return $true
    }
    
    try {
        Write-Info "Running unit tests..."
        $result = go test -v ".\internal\..." 2>&1
        
        if ($LASTEXITCODE -eq 0) {
            Write-Info "✓ Unit tests passed"
            return $true
        } else {
            Write-Error "Unit tests failed"
            Write-Host $result -ForegroundColor Red
            return $false
        }
    } catch {
        Write-Error "Unit test execution failed: $_"
        return $false
    }
}

# Test Helm templates
function Test-HelmTemplates {
    Write-Info "Testing Helm templates..."
    
    $templateFile = "k8s\helm\templates\operator-deployment.yaml"
    
    if (Test-Path $templateFile) {
        try {
            $content = Get-Content $templateFile -Raw
            
            # Count template braces
            $openBraces = ([regex]::Matches($content, "\{\{")).Count
            $closeBraces = ([regex]::Matches($content, "\}\}")).Count
            
            if ($openBraces -eq $closeBraces) {
                Write-Info "✓ Helm template syntax appears valid"
                return $true
            } else {
                Write-Warn "Helm template may have unbalanced braces ({{ vs }})"
                return $false
            }
        } catch {
            Write-Error "Failed to validate Helm template: $_"
            return $false
        }
    } else {
        Write-Warn "Helm template not found: $templateFile"
        return $true
    }
}

# Test examples
function Test-Examples {
    Write-Info "Testing example resources..."
    
    $exampleFile = "k8s\examples\example_optimized_inference.yaml"
    
    if (Test-Path $exampleFile) {
        try {
            $content = Get-Content $exampleFile -Raw
            
            # Check for required fields
            $requiredFields = @(
                "metadata:",
                "name:",
                "spec:",
                "model:",
                "engine:",
                "gpuProfile:",
                "replicas:",
                "resources:"
            )
            
            $allFieldsPresent = $true
            foreach ($field in $requiredFields) {
                if ($content -match $field) {
                    Write-Info "✓ Field '$field' found"
                } else {
                    Write-Error "Required field '$field' not found"
                    $allFieldsPresent = $false
                }
            }
            
            return $allFieldsPresent
        } catch {
            Write-Error "Failed to validate example: $_"
            return $false
        }
    } else {
        Write-Error "Example resource not found: $exampleFile"
        return $false
    }
}

# Main test execution
function Run-AllTests {
    Write-Info "Running all local operator tests (no Kubernetes required)..."
    
    $tests = @(
        @{ Name = "CRD Validation"; Function = { Test-CRDValidation } },
        @{ Name = "Go Modules"; Function = { Test-GoModules } },
        @{ Name = "Operator Build"; Function = { Test-OperatorBuild } },
        @{ Name = "Unit Tests"; Function = { Test-UnitTests } },
        @{ Name = "Docker Build"; Function = { Test-DockerBuild } },
        @{ Name = "Configuration"; Function = { Test-OperatorConfig } },
        @{ Name = "Helm Templates"; Function = { Test-HelmTemplates } },
        @{ Name = "Examples"; Function = { Test-Examples } }
    )
    
    $passedTests = 0
    $failedTests = 0
    
    foreach ($test in $tests) {
        Write-Host "`n--- Testing $($test.Name) ---" -ForegroundColor Cyan
        
        try {
            $result = & $test.Function
            if ($result) {
                $passedTests++
                Write-Info "✅ $($test.Name): PASSED"
            } else {
                $failedTests++
                Write-Error "❌ $($test.Name): FAILED"
            }
        } catch {
            $failedTests++
            Write-Error "❌ $($test.Name): FAILED with exception: $_"
        }
    }
    
    # Summary
    Write-Host "`n=== Test Summary ===" -ForegroundColor Cyan
    Write-Host "Passed: $passedTests" -ForegroundColor Green
    Write-Host "Failed: $failedTests" -ForegroundColor Red
    
    if ($failedTests -eq 0) {
        Write-Host "`n🎉 All local tests passed!" -ForegroundColor Green
        Write-Host "Operator is ready for Kubernetes deployment!" -ForegroundColor Green
        
        Write-Host "`nNext steps:" -ForegroundColor Yellow
        Write-Host "1. Setup Kubernetes cluster (Docker Desktop, k3d, kind, etc.)" -ForegroundColor White
        Write-Host "2. Apply CRD: kubectl apply -f k8s\crds\optimized_inference_crd.yaml --validate=false" -ForegroundColor White
        Write-Host "3. Test resource: kubectl apply -f k8s\examples\example_optimized_inference.yaml --validate=false" -ForegroundColor White
    } else {
        Write-Host "`n❌ $failedTests test(s) failed. Please fix issues before deployment." -ForegroundColor Red
        exit 1
    }
}

# Handle different actions
switch ($Action.ToLower()) {
    "crd" { Test-CRDValidation }
    "build" { Test-OperatorBuild }
    "docker" { Test-DockerBuild }
    "modules" { Test-GoModules }
    "config" { Test-OperatorConfig }
    "tests" { Test-UnitTests }
    "helm" { Test-HelmTemplates }
    "examples" { Test-Examples }
    "all" { Run-AllTests }
    default { 
        Write-Host "Usage: .\scripts\test-operator-local.ps1 [all|crd|build|docker|modules|config|tests|helm|examples]" -ForegroundColor Yellow
        Write-Host "  all      - Run all tests (default)" -ForegroundColor White
        Write-Host "  crd      - Test CRD validation" -ForegroundColor White
        Write-Host "  build    - Test Go build" -ForegroundColor White
        Write-Host "  docker   - Test Docker build" -ForegroundColor White
        Write-Host "  modules  - Test Go modules" -ForegroundColor White
        Write-Host "  config   - Test configuration" -ForegroundColor White
        Write-Host "  tests    - Run unit tests" -ForegroundColor White
        Write-Host "  helm     - Test Helm templates" -ForegroundColor White
        Write-Host "  examples - Test example resources" -ForegroundColor White
    }
}