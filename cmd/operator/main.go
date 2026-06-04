package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	inferboltv1alpha1 "github.com/inferbolthq/inferbolt/k8s/crds"
)

type OptimizedInferenceReconciler struct {
	client.Client
	OrchestratorURL string
	httpClient      *http.Client
}

func (r *OptimizedInferenceReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	// Step 1: Fetch the resource
	var oi inferboltv1alpha1.OptimizedInference
	if err := r.Get(ctx, req.NamespacedName, &oi); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// Step 2: Initialize phase if new resource
	if oi.Status.Phase == "" {
		oi.Status.Phase = "Pending"
		oi.Status.Message = "Initializing resource"
		if err := r.Status().Update(ctx, &oi); err != nil {
			slog.Error("failed to update status to Pending", "error", err)
			return reconcile.Result{}, err
		}
		return reconcile.Result{Requeue: true}, nil
	}

	// Step 3: Handle auto-optimization
	if oi.Spec.AutoOptimize && oi.Status.BenchmarkJobID == "" {
		jobID, err := r.submitBenchmarkJob(ctx, &oi)
		if err != nil {
			oi.Status.Phase = "Failed"
			oi.Status.Message = fmt.Sprintf("Failed to submit benchmark job: %v", err)
			r.Status().Update(ctx, &oi)
			return reconcile.Result{}, err
		}

		oi.Status.Phase = "Optimizing"
		oi.Status.BenchmarkJobID = jobID
		oi.Status.Message = "Running benchmark for optimization"
		if err := r.Status().Update(ctx, &oi); err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Step 4: Check optimization status
	if oi.Status.Phase == "Optimizing" {
		state, err := r.checkBenchmarkJob(ctx, oi.Status.BenchmarkJobID)
		if err != nil {
			slog.Error("failed to check benchmark job", "job_id", oi.Status.BenchmarkJobID, "error", err)
			return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
		}

		switch state {
		case "completed":
			// Get recommendations and apply them
			if err := r.applyOptimization(ctx, &oi); err != nil {
				oi.Status.Phase = "Failed"
				oi.Status.Message = fmt.Sprintf("Failed to apply optimization: %v", err)
			} else {
				oi.Status.Phase = "Deploying"
				oi.Status.Message = "Applying optimized configuration"
				now := metav1.Now()
				oi.Status.LastOptimizedAt = &now
				if configJSON, err := json.Marshal(oi.Spec); err == nil {
					oi.Status.CurrentConfig = string(configJSON)
				}
			}
			if err := r.Update(ctx, &oi); err != nil {
				return reconcile.Result{}, err
			}
			if err := r.Status().Update(ctx, &oi); err != nil {
				return reconcile.Result{}, err
			}
			return reconcile.Result{Requeue: true}, nil

		case "failed":
			oi.Status.Phase = "Failed"
			oi.Status.Message = "Benchmark job failed"
			r.Status().Update(ctx, &oi)
			return reconcile.Result{}, nil

		default: // still running
			return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
		}
	}

	// Step 5: Deploy or update deployment
	if oi.Status.Phase == "Deploying" || oi.Status.Phase == "Pending" {
		if err := r.reconcileDeployment(ctx, &oi); err != nil {
			oi.Status.Phase = "Failed"
			oi.Status.Message = fmt.Sprintf("Failed to deploy: %v", err)
			r.Status().Update(ctx, &oi)
			return reconcile.Result{}, err
		}

		oi.Status.Phase = "Running"
		oi.Status.Message = "Deployment is running"
		if err := r.Status().Update(ctx, &oi); err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	// Step 6: Resource is running, nothing to do
	return reconcile.Result{}, nil
}

type BenchmarkJobRequest struct {
	Model     string                       `json:"model"`
	Engines   []string                     `json:"engines"`
	Workload  BenchmarkWorkloadConfig      `json:"workload"`
	GPUProfile string                      `json:"gpu_profile"`
}

type BenchmarkWorkloadConfig struct {
	Concurrency   int `json:"concurrency"`
	PromptTokens  int `json:"prompt_tokens"`
	OutputTokens  int `json:"output_tokens"`
	NumRequests   int `json:"num_requests"`
}

type BenchmarkJobResponse struct {
	ID string `json:"id"`
}

func (r *OptimizedInferenceReconciler) submitBenchmarkJob(ctx context.Context, oi *inferboltv1alpha1.OptimizedInference) (string, error) {
	reqBody := BenchmarkJobRequest{
		Model:   oi.Spec.Model,
		Engines: []string{oi.Spec.Engine},
		Workload: BenchmarkWorkloadConfig{
			Concurrency:  32,
			PromptTokens: 512,
			OutputTokens: 256,
			NumRequests:  200,
		},
		GPUProfile: oi.Spec.GPUProfile,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal job request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", r.OrchestratorURL+"/v1/jobs", bytes.NewBuffer(payload))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("submit job: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("orchestrator returned status %d", resp.StatusCode)
	}

	var jobResp BenchmarkJobResponse
	if err := json.NewDecoder(resp.Body).Decode(&jobResp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	slog.Info("submitted benchmark job", "job_id", jobResp.ID, "resource", oi.Name)
	return jobResp.ID, nil
}

type JobStatus struct {
	State string `json:"state"`
}

func (r *OptimizedInferenceReconciler) checkBenchmarkJob(ctx context.Context, jobID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", r.OrchestratorURL+"/v1/jobs/"+jobID, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("check job status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("orchestrator returned status %d", resp.StatusCode)
	}

	var status JobStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return "", fmt.Errorf("decode status: %w", err)
	}

	return status.State, nil
}

type OptimizationResult struct {
	Quantization   string `json:"quantization"`
	TensorParallel int32  `json:"tensor_parallel"`
	MaxBatchSize   int32  `json:"max_batch_size"`
}

func (r *OptimizedInferenceReconciler) applyOptimization(ctx context.Context, oi *inferboltv1alpha1.OptimizedInference) error {
	req, err := http.NewRequestWithContext(ctx, "GET", r.OrchestratorURL+"/v1/jobs/"+oi.Status.BenchmarkJobID+"/results", nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("get results: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("orchestrator returned status %d", resp.StatusCode)
	}

	var result OptimizationResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode results: %w", err)
	}

	// Apply optimization to spec
	oi.Spec.Quantization = result.Quantization
	oi.Spec.TensorParallel = result.TensorParallel
	oi.Spec.MaxBatchSize = result.MaxBatchSize

	slog.Info("applied optimization", 
		"resource", oi.Name, 
		"quantization", result.Quantization,
		"tensor_parallel", result.TensorParallel,
		"max_batch_size", result.MaxBatchSize)

	return nil
}

func (r *OptimizedInferenceReconciler) reconcileDeployment(ctx context.Context, oi *inferboltv1alpha1.OptimizedInference) error {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "inferbolt-" + oi.Name,
			Namespace: oi.Namespace,
		},
	}

	return ctrl.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		// Set owner reference for garbage collection
		if err := ctrl.SetControllerReference(oi, deployment, r.Scheme()); err != nil {
			return err
		}

		deployment.Spec = appsv1.DeploymentSpec{
			Replicas: &oi.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "inferbolt-" + oi.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "inferbolt-" + oi.Name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "inference",
							Image: r.getContainerImage(oi.Spec.Engine),
							Args:  r.buildContainerArgs(oi),
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"nvidia.com/gpu": resource.MustParse(fmt.Sprintf("%d", oi.Spec.Resources.GPUCount)),
									"memory":         resource.MustParse(fmt.Sprintf("%dGi", oi.Spec.Resources.MemoryGB)),
									"cpu":            resource.MustParse(fmt.Sprintf("%dm", oi.Spec.Resources.CPUCores*1000)),
								},
								Requests: corev1.ResourceList{
									"nvidia.com/gpu": resource.MustParse(fmt.Sprintf("%d", oi.Spec.Resources.GPUCount)),
									"memory":         resource.MustParse(fmt.Sprintf("%dGi", oi.Spec.Resources.MemoryGB)),
									"cpu":            resource.MustParse(fmt.Sprintf("%dm", oi.Spec.Resources.CPUCores*1000)),
								},
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 8000,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt(8000),
									},
								},
								InitialDelaySeconds: 30,
								PeriodSeconds:       10,
							},
						},
					},
				},
			},
		}
		return nil
	})
}

func (r *OptimizedInferenceReconciler) getContainerImage(engine string) string {
	switch engine {
	case "vllm":
		return "vllm/vllm-openai:latest"
	case "sglang":
		return "lmsysorg/sglang:latest"
	case "tensorrt":
		return "nvcr.io/nvidia/tritonserver:24.01-trtllm-python-py3"
	case "llamacpp":
		return "ghcr.io/ggerganov/llama.cpp:server"
	default:
		return "vllm/vllm-openai:latest"
	}
}

func (r *OptimizedInferenceReconciler) buildContainerArgs(oi *inferboltv1alpha1.OptimizedInference) []string {
	args := []string{
		"--model", oi.Spec.Model,
		"--port", "8000",
		"--host", "0.0.0.0",
	}

	if oi.Spec.Quantization != "" {
		args = append(args, "--quantization", oi.Spec.Quantization)
	}

	if oi.Spec.TensorParallel > 0 {
		args = append(args, "--tensor-parallel-size", fmt.Sprintf("%d", oi.Spec.TensorParallel))
	}

	if oi.Spec.MaxBatchSize > 0 {
		args = append(args, "--max-num-seqs", fmt.Sprintf("%d", oi.Spec.MaxBatchSize))
	}

	if oi.Spec.MaxModelLen > 0 {
		args = append(args, "--max-model-len", fmt.Sprintf("%d", oi.Spec.MaxModelLen))
	}

	return args
}

func main() {
	// Configure structured logging
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	// Read environment variables
	orchestratorURL := os.Getenv("ORCHESTRATOR_URL")
	if orchestratorURL == "" {
		slog.Error("ORCHESTRATOR_URL is required")
		os.Exit(1)
	}

	// Register our types with the scheme
	if err := inferboltv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		slog.Error("failed to add types to scheme", "error", err)
		os.Exit(1)
	}

	// Create controller-runtime manager
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme.Scheme,
	})
	if err != nil {
		slog.Error("failed to create manager", "error", err)
		os.Exit(1)
	}

	// Create reconciler
	reconciler := &OptimizedInferenceReconciler{
		Client:          mgr.GetClient(),
		OrchestratorURL: orchestratorURL,
		httpClient:      &http.Client{Timeout: 30 * time.Second},
	}

	// Set up controller
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&inferboltv1alpha1.OptimizedInference{}).
		Owns(&appsv1.Deployment{}).
		Complete(reconciler); err != nil {
		slog.Error("failed to create controller", "error", err)
		os.Exit(1)
	}

	slog.Info("starting operator", "orchestrator_url", orchestratorURL)

	// Start the manager
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		slog.Error("manager exited with error", "error", err)
		os.Exit(1)
	}
}

