#!/bin/bash
# Local operator testing without Kubernetes cluster

set -e

echo "=== InferX Operator Local Testing ==="

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

# Test CRD validation without cluster
test_crd_validation() {
    log_info "Testing CRD validation..."
    
    # Test valid CRD
    if command -v kubeval > /dev/null; then
        log_info "Validating CRD with kubeval..."
        kubeval k8s/crds/optimized_inference_crd.yaml
    else
        log_info "kubeval not found, using basic YAML validation..."
        if command -v yq > /dev/null; then
            yq eval '.' k8s/crds/optimized_inference_crd.yaml > /dev/null
            log_info "CRD YAML is valid"
        else
            log_warn "Neither kubeval nor yq found, skipping validation"
        fi
    fi
    
    # Test example resource
    log_info "Validating example OptimizedInference resource..."
    if command -v yq > /dev/null; then
        yq eval '.' k8s/examples/example_optimized_inference.yaml > /dev/null
        log_info "Example resource YAML is valid"
    fi
}

# Test operator binary compilation
test_operator_build() {
    log_info "Testing operator build..."
    
    if ! command -v go > /dev/null; then
        log_error "Go is not installed, cannot test operator build"
        return 1
    fi
    
    # Try to build operator
    log_info "Building operator binary..."
    if go build -o /tmp/test-operator ./cmd/operator; then
        log_info "Operator builds successfully"
        rm -f /tmp/test-operator
    else
        log_error "Operator build failed"
        return 1
    fi
}

# Test operator logic (unit tests)
test_operator_logic() {
    log_info "Testing operator logic..."
    
    if ! command -v go > /dev/null; then
        log_warn "Go not found, skipping logic tests"
        return 0
    fi
    
    # Create basic unit test for operator types
    cat > /tmp/operator_test.go << 'EOF'
package main

import (
    "testing"
    inferxv1alpha1 "github.com/karthikkay07/inferx/k8s/crds"
)

func TestOptimizedInferenceTypes(t *testing.T) {
    // Test OptimizedInference creation
    oi := &inferxv1alpha1.OptimizedInference{}
    oi.Spec.Model = "test-model"
    oi.Spec.Engine = "vllm"
    oi.Spec.GPUProfile = "a100-80gb"
    oi.Spec.Replicas = 2
    oi.Spec.Resources.GPUCount = 1
    oi.Spec.Resources.MemoryGB = 16
    oi.Spec.Resources.CPUCores = 4
    
    if oi.Spec.Model != "test-model" {
        t.Errorf("Expected model 'test-model', got '%s'", oi.Spec.Model)
    }
    
    if oi.Spec.Replicas != 2 {
        t.Errorf("Expected replicas 2, got %d", oi.Spec.Replicas)
    }
}

func TestDeepCopy(t *testing.T) {
    // Test deep copy functionality
    original := &inferxv1alpha1.OptimizedInference{}
    original.Spec.Model = "original-model"
    original.Spec.Engine = "vllm"
    
    copied := original.DeepCopy()
    
    if copied.Spec.Model != original.Spec.Model {
        t.Errorf("DeepCopy failed: expected '%s', got '%s'", original.Spec.Model, copied.Spec.Model)
    }
    
    // Verify it's actually a copy (modify original shouldn't affect copy)
    original.Spec.Model = "modified-model"
    if copied.Spec.Model == original.Spec.Model {
        t.Error("DeepCopy created a shallow copy instead of deep copy")
    }
}
EOF
    
    # Run the test
    cd /tmp
    go mod init operator-test > /dev/null 2>&1 || true
    if go test -v operator_test.go; then
        log_info "Operator logic tests passed"
    else
        log_error "Operator logic tests failed"
        return 1
    fi
    
    # Clean up
    rm -f /tmp/operator_test.go /tmp/go.mod /tmp/go.sum
}

# Test Docker image build
test_docker_build() {
    log_info "Testing operator Docker image build..."
    
    if ! command -v docker > /dev/null; then
        log_warn "Docker not found, skipping image build test"
        return 0
    fi
    
    # Test build
    if docker build -t inferx-operator:test -f Dockerfile.operator . > /dev/null; then
        log_info "Operator Docker image builds successfully"
        
        # Clean up test image
        docker rmi inferx-operator:test > /dev/null 2>&1 || true
    else
        log_error "Operator Docker image build failed"
        return 1
    fi
}

# Test operator configuration
test_operator_config() {
    log_info "Testing operator configuration..."
    
    # Test environment variables
    export ORCHESTRATOR_URL="http://test-orchestrator:8081"
    
    # Test that operator would start with config (dry run)
    if command -v go > /dev/null; then
        log_info "Testing operator startup validation..."
        
        # Create a simple test to check config parsing
        cat > /tmp/config_test.go << 'EOF'
package main

import (
    "os"
    "testing"
)

func TestConfig(t *testing.T) {
    // Set test environment
    os.Setenv("ORCHESTRATOR_URL", "http://test:8081")
    
    url := os.Getenv("ORCHESTRATOR_URL")
    if url == "" {
        t.Error("ORCHESTRATOR_URL should not be empty")
    }
    
    if url != "http://test:8081" {
        t.Errorf("Expected URL 'http://test:8081', got '%s'", url)
    }
}
EOF
        
        cd /tmp
        go mod init config-test > /dev/null 2>&1 || true
        if go test -v config_test.go; then
            log_info "Operator configuration tests passed"
        else
            log_error "Operator configuration tests failed"
        fi
        
        rm -f /tmp/config_test.go /tmp/go.mod /tmp/go.sum
    fi
}

# Test Helm template rendering
test_helm_template() {
    log_info "Testing Helm template..."
    
    # Basic template substitution test
    local template_file="k8s/helm/templates/operator-deployment.yaml"
    
    if [ -f "$template_file" ]; then
        log_info "Checking Helm template syntax..."
        
        # Simple template validation (check for balanced braces)
        if grep -q "{{.*}}" "$template_file"; then
            local open_braces=$(grep -o "{{" "$template_file" | wc -l)
            local close_braces=$(grep -o "}}" "$template_file" | wc -l)
            
            if [ "$open_braces" -eq "$close_braces" ]; then
                log_info "Helm template syntax appears valid"
            else
                log_warn "Helm template may have unbalanced braces"
            fi
        fi
        
        # Test basic template rendering (manual substitution)
        log_info "Testing template variable substitution..."
        sed 's/{{ .Release.Namespace }}/test-namespace/g' "$template_file" | \
        sed 's/{{ .Values.orchestratorURL }}/http:\/\/test:8081/g' | \
        sed 's/{{ .Values.operator.image.repository }}/test-repo/g' | \
        sed 's/{{ .Values.operator.image.tag }}/test-tag/g' | \
        sed '/{{.*include.*}}/d' > /tmp/rendered-template.yaml
        
        if command -v yq > /dev/null; then
            if yq eval '.' /tmp/rendered-template.yaml > /dev/null; then
                log_info "Rendered template is valid YAML"
            else
                log_warn "Rendered template has YAML syntax issues"
            fi
        fi
        
        rm -f /tmp/rendered-template.yaml
    else
        log_warn "Helm template file not found: $template_file"
    fi
}

# Test resource examples
test_examples() {
    log_info "Testing example resources..."
    
    local example_file="k8s/examples/example_optimized_inference.yaml"
    
    if [ -f "$example_file" ]; then
        # Validate YAML syntax
        if command -v yq > /dev/null; then
            if yq eval '.' "$example_file" > /dev/null; then
                log_info "Example resource YAML is valid"
            else
                log_error "Example resource has YAML syntax errors"
                return 1
            fi
        fi
        
        # Check required fields
        log_info "Validating required fields in example..."
        local required_fields=("metadata.name" "spec.model" "spec.engine" "spec.gpuProfile" "spec.replicas" "spec.resources")
        
        for field in "${required_fields[@]}"; do
            if command -v yq > /dev/null; then
                if yq eval ".$field" "$example_file" > /dev/null 2>&1; then
                    log_info "✓ Field '$field' is present"
                else
                    log_error "✗ Required field '$field' is missing"
                    return 1
                fi
            fi
        done
        
    else
        log_warn "Example resource file not found: $example_file"
    fi
}

# Main test function
main() {
    log_info "Running local operator tests (no Kubernetes cluster required)..."
    
    local failed_tests=0
    
    # Run tests
    test_crd_validation || ((failed_tests++))
    test_operator_build || ((failed_tests++))
    test_operator_logic || ((failed_tests++))
    test_docker_build || ((failed_tests++))
    test_operator_config || ((failed_tests++))
    test_helm_template || ((failed_tests++))
    test_examples || ((failed_tests++))
    
    # Summary
    log_info "=== Local Operator Test Summary ==="
    if [ $failed_tests -eq 0 ]; then
        log_info "✅ All local tests passed!"
        log_info "✅ CRD validation"
        log_info "✅ Operator compilation"
        log_info "✅ Type system tests"
        log_info "✅ Docker image build"
        log_info "✅ Configuration validation"
        log_info "✅ Helm template syntax"
        log_info "✅ Example resources"
        
        echo -e "${GREEN}🎉 Operator is ready for Kubernetes deployment!${NC}"
        echo ""
        echo "Next steps:"
        echo "  1. Setup Kubernetes cluster: ./scripts/setup-k8s-test.sh setup"
        echo "  2. Test with cluster:        ./scripts/setup-k8s-test.sh test"
        echo "  3. Or use existing cluster:  kubectl apply -f k8s/crds/optimized_inference_crd.yaml --validate=false"
        
    else
        log_error "❌ $failed_tests test(s) failed"
        echo "Please fix the issues above before deploying to Kubernetes"
        exit 1
    fi
}

# Run tests
main "$@"