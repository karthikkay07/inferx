# PowerShell script for Kubernetes testing setup on Windows

param(
    [string]$Action = "help"
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

Write-Host "=== InferX Kubernetes Test Setup ===" -ForegroundColor Cyan

# Check if kubectl is available and configured
function Test-Kubectl {
    if (!(Get-Command kubectl -ErrorAction SilentlyContinue)) {
        Write-Error "kubectl is not installed. Please install kubectl first."
        Write-Host "Installation guide: https://kubernetes.io/docs/tasks/tools/install-kubectl-windows/" -ForegroundColor Yellow
        return $false
    }
    
    Write-Info "kubectl found: $(kubectl version --client --short 2>$null)"
    
    # Check if kubectl can connect to a cluster
    try {
        $clusterInfo = kubectl cluster-info 2>&1
        if ($LASTEXITCODE -eq 0) {
            Write-Info "kubectl is connected to cluster"
            return $true
        } else {
            Write-Warn "kubectl is not connected to any Kubernetes cluster"
            return $false
        }
    } catch {
        Write-Warn "kubectl connection test failed"
        return $false
    }
}

# Check for Docker Desktop Kubernetes
function Test-DockerDesktopK8s {
    Write-Info "Checking for Docker Desktop Kubernetes..."
    
    # Check if Docker Desktop is running
    if (!(Get-Command docker -ErrorAction SilentlyContinue)) {
        Write-Warn "Docker not found"
        return $false
    }
    
    # Check if Kubernetes is enabled in Docker Desktop
    try {
        $contexts = kubectl config get-contexts --no-headers 2>$null
        if ($contexts -match "docker-desktop") {
            Write-Info "Docker Desktop Kubernetes found"
            return $true
        } else {
            Write-Info "Docker Desktop Kubernetes not enabled"
            return $false
        }
    } catch {
        return $false
    }
}

# Install and setup k3d
function Install-K3d {
    Write-Info "Setting up k3d for local Kubernetes testing..."
    
    # Check if k3d is already installed
    if (Get-Command k3d -ErrorAction SilentlyContinue) {
        Write-Info "k3d is already installed: $(k3d version)"
    } else {
        Write-Info "Installing k3d..."
        
        # Download k3d for Windows
        $k3dUrl = "https://github.com/k3d-io/k3d/releases/latest/download/k3d-windows-amd64.exe"
        $k3dPath = "$env:TEMP\k3d.exe"
        
        try {
            Write-Info "Downloading k3d..."
            Invoke-WebRequest -Uri $k3dUrl -OutFile $k3dPath -UseBasicParsing
            
            # Move to a directory in PATH
            $binDir = "$env:USERPROFILE\bin"
            if (!(Test-Path $binDir)) {
                New-Item -ItemType Directory -Path $binDir -Force | Out-Null
            }
            
            Move-Item $k3dPath "$binDir\k3d.exe" -Force
            
            # Add to PATH if not already there
            $currentPath = [Environment]::GetEnvironmentVariable("PATH", [EnvironmentVariableTarget]::User)
            if ($currentPath -notlike "*$binDir*") {
                [Environment]::SetEnvironmentVariable("PATH", "$currentPath;$binDir", [EnvironmentVariableTarget]::User)
                $env:PATH += ";$binDir"
            }
            
            Write-Info "k3d installed successfully"
        } catch {
            Write-Error "Failed to install k3d: $_"
            return $false
        }
    }
    
    # Create k3d cluster
    Write-Info "Creating k3d cluster 'inferx-test'..."
    
    # Delete existing cluster if it exists
    k3d cluster delete inferx-test 2>$null | Out-Null
    
    try {
        # Create new cluster
        k3d cluster create inferx-test --agents 1 --wait --timeout 5m
        
        # Verify cluster is ready
        kubectl cluster-info
        kubectl get nodes
        
        Write-Info "k3d cluster 'inferx-test' created successfully"
        return $true
    } catch {
        Write-Error "Failed to create k3d cluster: $_"
        return $false
    }
}

# Install CRDs and operator
function Install-Operator {
    Write-Info "Installing InferX Kubernetes components..."
    
    # Apply CRD (skip validation initially)
    Write-Info "Installing OptimizedInference CRD..."
    try {
        kubectl apply -f k8s\crds\optimized_inference_crd.yaml --validate=false
        
        # Wait for CRD to be established
        Write-Info "Waiting for CRD to be ready..."
        $timeout = 60
        $elapsed = 0
        do {
            $crdReady = kubectl get crd optimizedinferences.inferx.io -o jsonpath='{.status.conditions[?(@.type=="Established")].status}' 2>$null
            if ($crdReady -eq "True") {
                Write-Info "CRD is ready"
                break
            }
            Start-Sleep 2
            $elapsed += 2
        } while ($elapsed -lt $timeout)
        
        if ($elapsed -ge $timeout) {
            Write-Warn "CRD may not be fully ready, continuing anyway..."
        }
    } catch {
        Write-Error "Failed to install CRD: $_"
        return $false
    }
    
    # Create namespace
    try {
        kubectl create namespace inferx-system --dry-run=client -o yaml | kubectl apply -f -
        Write-Info "Created namespace: inferx-system"
    } catch {
        Write-Warn "Failed to create namespace, it may already exist"
    }
    
    # Build operator image if needed
    if (Get-Command docker -ErrorAction SilentlyContinue) {
        Write-Info "Building operator Docker image..."
        docker build -t inferx-operator:latest -f Dockerfile.operator .
        
        # Load image into k3d cluster if using k3d
        if (Get-Command k3d -ErrorAction SilentlyContinue) {
            $clusters = k3d cluster list --no-headers 2>$null
            if ($clusters -match "inferx-test") {
                Write-Info "Loading image into k3d cluster..."
                k3d image import inferx-operator:latest -c inferx-test
            }
        }
    } else {
        Write-Warn "Docker not found, operator image build skipped"
    }
    
    # Apply operator deployment
    Write-Info "Installing InferX operator..."
    try {
        # Create a modified deployment YAML
        $deploymentContent = Get-Content k8s\helm\templates\operator-deployment.yaml -Raw
        
        # Replace Helm template variables
        $deploymentContent = $deploymentContent -replace '\{\{ \.Release\.Namespace \}\}', 'inferx-system'
        $deploymentContent = $deploymentContent -replace '\{\{ \.Values\.orchestratorURL \| quote \}\}', '"http://host.docker.internal:8081"'
        $deploymentContent = $deploymentContent -replace '\{\{ \.Values\.operator\.image\.repository \}\}', 'inferx-operator'
        $deploymentContent = $deploymentContent -replace '\{\{ \.Values\.operator\.image\.tag \}\}', 'latest'
        
        # Remove lines with remaining template functions
        $deploymentContent = ($deploymentContent -split "`n" | Where-Object { $_ -notmatch '\{\{.*include.*\}\}' }) -join "`n"
        
        # Apply the deployment
        $deploymentContent | kubectl apply -f - --namespace=inferx-system
        
        Write-Info "Operator deployment created"
        return $true
    } catch {
        Write-Error "Failed to install operator: $_"
        return $false
    }
}

# Test the operator
function Test-Operator {
    Write-Info "Testing InferX operator..."
    
    # Apply test resource
    Write-Info "Creating OptimizedInference resource..."
    try {
        kubectl apply -f k8s\examples\example_optimized_inference.yaml --validate=false
        
        # Wait a moment for operator to process
        Start-Sleep 5
        
        # Check resource status
        Write-Info "Resource status:"
        kubectl get optimizedinferences -o wide
        
        Write-Info "Resource details:"
        kubectl describe optimizedinference llama3-production
        
        # Check if deployment was created
        Write-Info "Checking for created deployments..."
        kubectl get deployments
        
        # Check operator logs
        Write-Info "Recent operator logs:"
        $operatorLogs = kubectl logs -l app=inferx-operator -n inferx-system --tail=20 2>$null
        if ($operatorLogs) {
            Write-Host $operatorLogs -ForegroundColor Gray
        } else {
            Write-Warn "No operator logs found (operator may still be starting)"
        }
        
        Write-Info "Operator test completed"
        return $true
    } catch {
        Write-Error "Operator test failed: $_"
        return $false
    }
}

# Cleanup function
function Remove-TestEnvironment {
    Write-Info "Cleaning up Kubernetes test environment..."
    
    # Delete test resources
    kubectl delete optimizedinference --all --ignore-not-found=true 2>$null | Out-Null
    kubectl delete namespace inferx-system --ignore-not-found=true 2>$null | Out-Null
    
    # Delete CRD
    kubectl delete -f k8s\crds\optimized_inference_crd.yaml --ignore-not-found=true 2>$null | Out-Null
    
    # Delete k3d cluster if it exists
    if (Get-Command k3d -ErrorAction SilentlyContinue) {
        k3d cluster delete inferx-test 2>$null | Out-Null
    }
    
    Write-Info "Cleanup completed"
}

# Show help
function Show-Help {
    Write-Host "Usage: .\scripts\setup-k8s-test.ps1 [action]" -ForegroundColor Yellow
    Write-Host ""
    Write-Host "Actions:" -ForegroundColor Cyan
    Write-Host "  setup   - Setup Kubernetes test environment" -ForegroundColor White
    Write-Host "  test    - Test the operator with sample resources" -ForegroundColor White
    Write-Host "  clean   - Clean up test environment" -ForegroundColor White
    Write-Host "  check   - Check current Kubernetes setup" -ForegroundColor White
    Write-Host "  help    - Show this help" -ForegroundColor White
    Write-Host ""
    Write-Host "Examples:" -ForegroundColor Cyan
    Write-Host "  .\scripts\setup-k8s-test.ps1 setup" -ForegroundColor Gray
    Write-Host "  .\scripts\setup-k8s-test.ps1 test" -ForegroundColor Gray
}

# Check current setup
function Check-Setup {
    Write-Info "Checking Kubernetes setup..."
    
    Write-Host "`n--- kubectl Status ---" -ForegroundColor Cyan
    if (Test-Kubectl) {
        try {
            $context = kubectl config current-context
            Write-Info "Current context: $context"
            
            $nodes = kubectl get nodes --no-headers 2>$null
            Write-Info "Nodes: $($nodes.Count) found"
            
            $namespaces = kubectl get namespaces --no-headers 2>$null
            Write-Info "Namespaces: $($namespaces.Count) found"
        } catch {
            Write-Warn "Error getting cluster info"
        }
    }
    
    Write-Host "`n--- Docker Desktop ---" -ForegroundColor Cyan
    if (Test-DockerDesktopK8s) {
        Write-Info "Docker Desktop Kubernetes is available"
    } else {
        Write-Info "Docker Desktop Kubernetes not enabled"
        Write-Host "To enable: Docker Desktop > Settings > Kubernetes > Enable Kubernetes" -ForegroundColor Yellow
    }
    
    Write-Host "`n--- k3d ---" -ForegroundColor Cyan
    if (Get-Command k3d -ErrorAction SilentlyContinue) {
        $clusters = k3d cluster list --no-headers 2>$null
        if ($clusters) {
            Write-Info "k3d clusters found: $($clusters.Count)"
        } else {
            Write-Info "No k3d clusters found"
        }
    } else {
        Write-Info "k3d not installed"
    }
    
    Write-Host "`n--- InferX CRDs ---" -ForegroundColor Cyan
    try {
        $crd = kubectl get crd optimizedinferences.inferx.io --no-headers 2>$null
        if ($crd) {
            Write-Info "InferX CRD is installed"
        } else {
            Write-Info "InferX CRD not found"
        }
    } catch {
        Write-Info "Cannot check CRDs (no cluster connection)"
    }
}

# Main execution
switch ($Action.ToLower()) {
    "setup" {
        Write-Info "Setting up Kubernetes test environment..."
        
        # Try to use existing cluster first
        if (Test-Kubectl) {
            Write-Info "Using existing Kubernetes cluster"
            Install-Operator
        } elseif (Test-DockerDesktopK8s) {
            Write-Info "Using Docker Desktop Kubernetes"
            kubectl config use-context docker-desktop
            Install-Operator
        } else {
            Write-Info "No Kubernetes cluster found. Setting up k3d..."
            if (Install-K3d) {
                Install-Operator
            } else {
                Write-Error "Failed to setup Kubernetes environment"
                Write-Host "`nManual setup options:" -ForegroundColor Yellow
                Write-Host "1. Enable Docker Desktop Kubernetes: Settings > Kubernetes > Enable" -ForegroundColor White
                Write-Host "2. Install k3d manually and run: k3d cluster create inferx-test" -ForegroundColor White
                exit 1
            }
        }
        
        Write-Host "`n🎉 Kubernetes test environment ready!" -ForegroundColor Green
    }
    
    "test" {
        if (!(Test-Kubectl)) {
            Write-Error "No Kubernetes cluster connection. Run setup first."
            exit 1
        }
        Test-Operator
    }
    
    "clean" {
        Remove-TestEnvironment
    }
    
    "check" {
        Check-Setup
    }
    
    "help" {
        Show-Help
    }
    
    default {
        Write-Error "Unknown action: $Action"
        Show-Help
        exit 1
    }
}