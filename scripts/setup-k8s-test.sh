#!/bin/bash
# Kubernetes testing setup script for InferX

set -e

echo "=== Kubernetes Test Environment Setup ==="

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

# Check if kubectl is available and configured
check_kubectl() {
    if ! command -v kubectl > /dev/null; then
        log_error "kubectl is not installed. Please install kubectl first."
        echo "Installation guide: https://kubernetes.io/docs/tasks/tools/"
        return 1
    fi
    
    # Check if kubectl can connect to a cluster
    if kubectl cluster-info > /dev/null 2>&1; then
        log_info "kubectl is connected to: $(kubectl config current-context)"
        return 0
    else
        log_warn "kubectl is not connected to any Kubernetes cluster"
        return 1
    fi
}

# Setup k3d cluster (lightweight Kubernetes)
setup_k3d() {
    if ! command -v k3d > /dev/null; then
        log_info "Installing k3d..."
        
        # Install k3d based on OS
        if [[ "$OSTYPE" == "linux-gnu"* ]]; then
            curl -s https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash
        elif [[ "$OSTYPE" == "darwin"* ]]; then
            if command -v brew > /dev/null; then
                brew install k3d
            else
                curl -s https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash
            fi
        elif [[ "$OSTYPE" == "msys" ]] || [[ "$OSTYPE" == "cygwin" ]]; then
            log_error "Please install k3d manually on Windows: https://k3d.io/v5.4.6/#installation"
            log_info "Alternative: Use Docker Desktop with Kubernetes enabled"
            return 1
        else
            log_error "Unsupported OS for automatic k3d installation"
            return 1
        fi
    fi
    
    # Create k3d cluster
    log_info "Creating k3d cluster 'inferx-test'..."
    
    # Stop existing cluster if it exists
    k3d cluster delete inferx-test > /dev/null 2>&1 || true
    
    # Create new cluster
    k3d cluster create inferx-test \
        --agents 1 \
        --port "8080:80@loadbalancer" \
        --port "8443:443@loadbalancer" \
        --wait
    
    # Verify cluster is ready
    kubectl cluster-info
    kubectl get nodes
    
    log_info "k3d cluster 'inferx-test' created successfully"
}

# Setup kind cluster (alternative to k3d)
setup_kind() {
    if ! command -v kind > /dev/null; then
        log_info "Installing kind..."
        
        # Install kind
        if [[ "$OSTYPE" == "linux-gnu"* ]]; then
            curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.20.0/kind-linux-amd64
            chmod +x ./kind
            sudo mv ./kind /usr/local/bin/kind
        elif [[ "$OSTYPE" == "darwin"* ]]; then
            if command -v brew > /dev/null; then
                brew install kind
            else
                curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.20.0/kind-darwin-amd64
                chmod +x ./kind
                sudo mv ./kind /usr/local/bin/kind
            fi
        elif [[ "$OSTYPE" == "msys" ]] || [[ "$OSTYPE" == "cygwin" ]]; then
            log_error "Please install kind manually on Windows: https://kind.sigs.k8s.io/docs/user/quick-start/#installation"
            return 1
        fi
    fi
    
    # Create kind cluster
    log_info "Creating kind cluster 'inferx-test'..."
    
    # Delete existing cluster if it exists
    kind delete cluster --name inferx-test > /dev/null 2>&1 || true
    
    # Create cluster config
    cat > /tmp/kind-config.yaml << EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  kubeadmConfigPatches:
  - |
    kind: InitConfiguration
    nodeRegistration:
      kubeletExtraArgs:
        node-labels: "ingress-ready=true"
- role: worker
EOF

    # Create new cluster
    kind create cluster --name inferx-test --config /tmp/kind-config.yaml --wait 5m
    
    # Set kubectl context
    kubectl cluster-info --context kind-inferx-test
    
    log_info "kind cluster 'inferx-test' created successfully"
}

# Install CRDs and operator
install_operator() {
    log_info "Installing InferX Kubernetes components..."
    
    # Apply CRD (skip validation initially)
    log_info "Installing OptimizedInference CRD..."
    kubectl apply -f k8s/crds/optimized_inference_crd.yaml --validate=false
    
    # Wait for CRD to be established
    log_info "Waiting for CRD to be ready..."
    kubectl wait --for condition=established --timeout=60s crd/optimizedinferences.inferx.io
    
    # Create namespace
    kubectl create namespace inferx-system --dry-run=client -o yaml | kubectl apply -f -
    
    # Apply operator deployment
    log_info "Installing InferX operator..."
    
    # Create configmap for operator configuration
    kubectl create configmap operator-config \
        --from-literal=ORCHESTRATOR_URL="http://host.docker.internal:8081" \
        --namespace=inferx-system \
        --dry-run=client -o yaml | kubectl apply -f -
    
    # Apply operator with namespace
    sed 's/{{ .Release.Namespace }}/inferx-system/g' k8s/helm/templates/operator-deployment.yaml | \
    sed 's/{{ .Values.orchestratorURL }}/http:\/\/host.docker.internal:8081/g' | \
    sed 's/{{ .Values.operator.image.repository }}/inferx-operator/g' | \
    sed 's/{{ .Values.operator.image.tag }}/latest/g' | \
    sed '/{{.*}}/d' | \
    kubectl apply -f - --namespace=inferx-system
    
    log_info "Kubernetes components installed successfully"
}

# Test the operator
test_operator() {
    log_info "Testing InferX operator..."
    
    # Apply test resource
    log_info "Creating OptimizedInference resource..."
    kubectl apply -f k8s/examples/example_optimized_inference.yaml --validate=false
    
    # Wait a moment for operator to process
    sleep 5
    
    # Check resource status
    log_info "Checking resource status..."
    kubectl get optimizedinferences -o wide
    kubectl describe optimizedinference llama3-production
    
    # Check if deployment was created
    log_info "Checking created deployment..."
    kubectl get deployments
    
    # Check operator logs
    log_info "Operator logs:"
    kubectl logs -l app=inferx-operator -n inferx-system --tail=20 || log_warn "Operator not yet running"
    
    log_info "Operator test completed"
}

# Build and load operator image (for local testing)
build_operator_image() {
    log_info "Building operator Docker image..."
    
    # Build operator image
    docker build -t inferx-operator:latest -f Dockerfile.operator .
    
    # Load image into cluster
    if command -v k3d > /dev/null && k3d cluster list | grep -q inferx-test; then
        log_info "Loading image into k3d cluster..."
        k3d image import inferx-operator:latest -c inferx-test
    elif command -v kind > /dev/null && kind get clusters | grep -q inferx-test; then
        log_info "Loading image into kind cluster..."
        kind load docker-image inferx-operator:latest --name inferx-test
    else
        log_warn "Cannot load image into cluster. Make sure to push to a registry for remote clusters."
    fi
}

# Cleanup function
cleanup() {
    log_info "Cleaning up Kubernetes test environment..."
    
    # Delete test resources
    kubectl delete optimizedinference --all --ignore-not-found=true
    kubectl delete namespace inferx-system --ignore-not-found=true
    
    # Delete CRD
    kubectl delete -f k8s/crds/optimized_inference_crd.yaml --ignore-not-found=true
    
    # Delete cluster
    if command -v k3d > /dev/null; then
        k3d cluster delete inferx-test > /dev/null 2>&1 || true
    fi
    
    if command -v kind > /dev/null; then
        kind delete cluster --name inferx-test > /dev/null 2>&1 || true
    fi
    
    log_info "Cleanup completed"
}

# Main function
main() {
    case "${1:-setup}" in
        "setup")
            log_info "Setting up Kubernetes test environment..."
            
            # Try to use existing cluster first
            if check_kubectl; then
                log_info "Using existing Kubernetes cluster"
            else
                log_info "No Kubernetes cluster found. Setting up local cluster..."
                
                # Try k3d first, fall back to kind
                if setup_k3d; then
                    log_info "Using k3d cluster"
                elif setup_kind; then
                    log_info "Using kind cluster"
                else
                    log_error "Failed to setup local Kubernetes cluster"
                    log_info "Please set up a Kubernetes cluster manually:"
                    log_info "  - Docker Desktop: Enable Kubernetes in settings"
                    log_info "  - minikube: minikube start"
                    log_info "  - k3d: k3d cluster create inferx-test"
                    log_info "  - kind: kind create cluster --name inferx-test"
                    exit 1
                fi
            fi
            
            # Build and install operator
            build_operator_image
            install_operator
            ;;
            
        "test")
            log_info "Testing Kubernetes operator..."
            test_operator
            ;;
            
        "build")
            log_info "Building operator image..."
            build_operator_image
            ;;
            
        "clean")
            cleanup
            ;;
            
        "help"|*)
            echo "Usage: $0 [setup|test|build|clean|help]"
            echo "  setup  - Setup Kubernetes test environment (default)"
            echo "  test   - Test the operator with sample resources"
            echo "  build  - Build and load operator Docker image"
            echo "  clean  - Clean up test environment"
            echo "  help   - Show this help"
            ;;
    esac
}

# Run if executed directly
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
    main "$@"
fi