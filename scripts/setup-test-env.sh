#!/bin/bash
# Test environment setup script for InferX

set -e

echo "=== InferX Test Environment Setup ==="

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

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."
    
    # Check Docker
    if ! command -v docker > /dev/null; then
        log_error "Docker is not installed. Please install Docker first."
        exit 1
    fi
    
    # Check Docker Compose
    if ! command -v docker-compose > /dev/null; then
        log_error "Docker Compose is not installed. Please install Docker Compose first."
        exit 1
    fi
    
    # Check Go
    if ! command -v go > /dev/null; then
        log_warn "Go is not installed. Some tests may not work."
    else
        log_info "Go version: $(go version)"
    fi
    
    # Check curl
    if ! command -v curl > /dev/null; then
        log_error "curl is not installed. Please install curl."
        exit 1
    fi
    
    # Check jq (optional)
    if ! command -v jq > /dev/null; then
        log_warn "jq is not installed. JSON output will not be formatted."
    fi
    
    log_info "Prerequisites check completed"
}

# Setup directory structure
setup_directories() {
    log_info "Setting up directory structure..."
    
    mkdir -p bin/
    mkdir -p logs/
    mkdir -p tmp/
    
    # Make scripts executable
    chmod +x scripts/*.sh
    
    log_info "Directory structure created"
}

# Build binaries
build_binaries() {
    if command -v go > /dev/null; then
        log_info "Building Go binaries..."
        
        go mod tidy
        
        mkdir -p bin/
        go build -o bin/router ./cmd/router
        go build -o bin/collector ./cmd/collector
        go build -o bin/operator ./cmd/operator
        
        if [ -d "cmd/gateway" ]; then
            go build -o bin/gateway ./cmd/gateway
        fi
        
        if [ -d "cmd/orchestrator" ]; then
            go build -o bin/orchestrator ./cmd/orchestrator
        fi
        
        log_info "Binaries built successfully"
    else
        log_warn "Go not found, skipping binary build"
    fi
}

# Setup test data
setup_test_data() {
    log_info "Setting up test data..."
    
    # Create test configuration files
    cat > tmp/test-config.env << EOF
# Test environment configuration
DATABASE_URL=postgres://inferx:inferx@localhost:5432/inferx
ORCHESTRATOR_URL=http://localhost:8081
ROUTER_PORT=8082
COLLECTOR_PORT=8083
SLACK_WEBHOOK_URL=
DRIFT_CHECK_INTERVAL=30s
DRIFT_LOOKBACK=5m
DRIFT_WARNING_PCT=10.0
DRIFT_CRITICAL_PCT=20.0
EOF
    
    # Create sample test requests
    cat > tmp/sample-requests.json << EOF
{
  "chat_workload": {
    "prompt_tokens": 100,
    "output_tokens": 50,
    "concurrency": 10,
    "structured_output": false,
    "tool_calls": false,
    "shared_prefix_ratio": 0.1,
    "gpu_profile": "a100-80gb",
    "tenant_id": "test-chat"
  },
  "agent_workload": {
    "prompt_tokens": 200,
    "output_tokens": 100,
    "concurrency": 5,
    "structured_output": true,
    "tool_calls": true,
    "shared_prefix_ratio": 0.2,
    "gpu_profile": "h100",
    "tenant_id": "test-agent"
  },
  "batch_workload": {
    "prompt_tokens": 2000,
    "output_tokens": 1000,
    "concurrency": 100,
    "structured_output": false,
    "tool_calls": false,
    "shared_prefix_ratio": 0.1,
    "gpu_profile": "a100-80gb",
    "tenant_id": "test-batch"
  },
  "rag_workload": {
    "prompt_tokens": 500,
    "output_tokens": 200,
    "concurrency": 20,
    "structured_output": false,
    "tool_calls": false,
    "shared_prefix_ratio": 0.6,
    "gpu_profile": "a100-80gb",
    "tenant_id": "test-rag"
  }
}
EOF
    
    log_info "Test data created"
}

# Setup Kubernetes testing (optional)
setup_kubernetes() {
    if command -v kubectl > /dev/null; then
        log_info "Setting up Kubernetes test environment..."
        
        # Check if k3d is available
        if command -v k3d > /dev/null; then
            log_info "Creating k3d cluster for testing..."
            k3d cluster create inferx-test --agents 1 --wait || log_warn "k3d cluster may already exist"
            
            # Apply CRDs
            kubectl apply -f k8s/crds/optimized_inference_crd.yaml
            
            log_info "Kubernetes test environment ready"
        elif command -v kind > /dev/null; then
            log_info "Creating kind cluster for testing..."
            kind create cluster --name inferx-test || log_warn "kind cluster may already exist"
            
            # Apply CRDs
            kubectl apply -f k8s/crds/optimized_inference_crd.yaml
            
            log_info "Kubernetes test environment ready"
        else
            log_warn "Neither k3d nor kind found. Kubernetes tests will require manual cluster setup."
        fi
    else
        log_warn "kubectl not found. Skipping Kubernetes setup."
    fi
}

# Install testing tools
install_test_tools() {
    log_info "Installing testing tools..."
    
    # Install hey for load testing
    if command -v go > /dev/null && ! command -v hey > /dev/null; then
        log_info "Installing hey load testing tool..."
        go install github.com/rakyll/hey@latest
        export PATH=$PATH:$(go env GOPATH)/bin
    fi
    
    # Check for Apache Bench
    if ! command -v ab > /dev/null; then
        log_warn "Apache Bench (ab) not found. Install with: apt-get install apache2-utils (Ubuntu/Debian)"
    fi
    
    log_info "Testing tools setup completed"
}

# Verify environment
verify_environment() {
    log_info "Verifying test environment..."
    
    # Check if Docker daemon is running
    if ! docker info > /dev/null 2>&1; then
        log_error "Docker daemon is not running. Please start Docker."
        exit 1
    fi
    
    # Check available ports
    local ports=(5432 8081 8082 8083)
    for port in "${ports[@]}"; do
        if lsof -i ":$port" > /dev/null 2>&1; then
            log_warn "Port $port is already in use. Tests may fail."
        fi
    done
    
    # Check disk space
    local free_space=$(df . | awk 'NR==2 {print $4}')
    if [ "$free_space" -lt 1000000 ]; then # Less than ~1GB
        log_warn "Low disk space. Docker operations may fail."
    fi
    
    log_info "Environment verification completed"
}

# Create test runner script
create_test_runner() {
    cat > run-tests.sh << 'EOF'
#!/bin/bash
# Test runner script

set -e

echo "=== InferX Test Runner ==="

# Function to run a specific test
run_test() {
    local test_name=$1
    local test_script=$2
    
    echo "Running $test_name..."
    if [ -f "$test_script" ]; then
        chmod +x "$test_script"
        if ./"$test_script"; then
            echo "✅ $test_name: PASSED"
        else
            echo "❌ $test_name: FAILED"
            return 1
        fi
    else
        echo "⚠️  $test_name: Script not found ($test_script)"
        return 1
    fi
}

# Parse command line arguments
case "${1:-all}" in
    "unit")
        echo "Running unit tests only..."
        make test-unit
        ;;
    "integration")
        echo "Running integration tests only..."
        run_test "Integration Tests" "scripts/test-integration.sh"
        ;;
    "load")
        echo "Running load tests only..."
        run_test "Load Tests" "scripts/test-load.sh"
        ;;
    "all")
        echo "Running all tests..."
        
        # Unit tests
        if command -v go > /dev/null; then
            echo "Running unit tests..."
            make test-unit
        else
            echo "⚠️  Skipping unit tests (Go not found)"
        fi
        
        # Integration tests
        run_test "Integration Tests" "scripts/test-integration.sh"
        
        # Load tests
        run_test "Load Tests" "scripts/test-load.sh"
        
        echo "🎉 All tests completed!"
        ;;
    "help"|*)
        echo "Usage: $0 [unit|integration|load|all]"
        echo "  unit        - Run unit tests only"
        echo "  integration - Run integration tests only"
        echo "  load        - Run load tests only"
        echo "  all         - Run all tests (default)"
        ;;
esac
EOF

    chmod +x run-tests.sh
    log_info "Test runner script created: run-tests.sh"
}

# Main setup function
main() {
    log_info "Starting InferX test environment setup..."
    
    check_prerequisites
    setup_directories
    build_binaries
    setup_test_data
    install_test_tools
    verify_environment
    create_test_runner
    
    # Optional Kubernetes setup
    if [ "${SETUP_K8S:-false}" = "true" ]; then
        setup_kubernetes
    fi
    
    log_info "=== Setup Summary ==="
    log_info "✅ Prerequisites checked"
    log_info "✅ Directory structure created"
    log_info "✅ Binaries built (if Go available)"
    log_info "✅ Test data prepared"
    log_info "✅ Testing tools installed"
    log_info "✅ Environment verified"
    log_info "✅ Test runner created"
    
    echo -e "${GREEN}🎉 Test environment setup completed!${NC}"
    echo ""
    echo "Next steps:"
    echo "  1. Start services: make start"
    echo "  2. Run tests: ./run-tests.sh"
    echo "  3. Check status: make test-health"
    echo ""
    echo "Available commands:"
    echo "  make help         - Show all available targets"
    echo "  make dev          - Start development environment"
    echo "  make test-unit    - Run unit tests"
    echo "  ./run-tests.sh    - Run all tests"
    echo "  make logs         - View service logs"
    echo "  make clean        - Clean up"
}

# Run setup if script is executed directly
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
    main "$@"
fi
EOF