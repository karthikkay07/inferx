package inferboltv1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type OptimizedInferenceSpec struct {
	Model          string       `json:"model" yaml:"model"`
	Engine         string       `json:"engine" yaml:"engine"`
	GPUProfile     string       `json:"gpuProfile" yaml:"gpuProfile"`
	Replicas       int32        `json:"replicas" yaml:"replicas"`
	Quantization   string       `json:"quantization,omitempty" yaml:"quantization,omitempty"`
	TensorParallel int32        `json:"tensorParallel,omitempty" yaml:"tensorParallel,omitempty"`
	MaxBatchSize   int32        `json:"maxBatchSize,omitempty" yaml:"maxBatchSize,omitempty"`
	MaxModelLen    int32        `json:"maxModelLen,omitempty" yaml:"maxModelLen,omitempty"`
	Resources      ResourceSpec `json:"resources" yaml:"resources"`
	AutoOptimize   bool         `json:"autoOptimize,omitempty" yaml:"autoOptimize,omitempty"`
}

type ResourceSpec struct {
	GPUCount int32 `json:"gpuCount" yaml:"gpuCount"`
	MemoryGB int32 `json:"memoryGB" yaml:"memoryGB"`
	CPUCores int32 `json:"cpuCores" yaml:"cpuCores"`
}

type OptimizedInferenceStatus struct {
	Phase              string       `json:"phase"`
	Message            string       `json:"message,omitempty"`
	LastOptimizedAt    *metav1.Time `json:"lastOptimizedAt,omitempty"`
	CurrentConfig      string       `json:"currentConfig,omitempty"`
	BenchmarkJobID     string       `json:"benchmarkJobID,omitempty"`
	ObservedGeneration int64        `json:"observedGeneration,omitempty"`
}

type OptimizedInference struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              OptimizedInferenceSpec   `json:"spec"`
	Status            OptimizedInferenceStatus `json:"status,omitempty"`
}

type OptimizedInferenceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OptimizedInference `json:"items"`
}

func (o *OptimizedInference) DeepCopyObject() runtime.Object {
	return o.DeepCopy()
}

func (o *OptimizedInferenceList) DeepCopyObject() runtime.Object {
	return o.DeepCopy()
}

func (in *OptimizedInference) DeepCopy() *OptimizedInference {
	if in == nil {
		return nil
	}
	out := new(OptimizedInference)
	in.DeepCopyInto(out)
	return out
}

func (in *OptimizedInference) DeepCopyInto(out *OptimizedInference) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *OptimizedInferenceSpec) DeepCopy() *OptimizedInferenceSpec {
	if in == nil {
		return nil
	}
	out := new(OptimizedInferenceSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *OptimizedInferenceSpec) DeepCopyInto(out *OptimizedInferenceSpec) {
	*out = *in
	out.Resources = in.Resources
}

func (in *ResourceSpec) DeepCopy() *ResourceSpec {
	if in == nil {
		return nil
	}
	out := new(ResourceSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *ResourceSpec) DeepCopyInto(out *ResourceSpec) {
	*out = *in
}

func (in *OptimizedInferenceStatus) DeepCopy() *OptimizedInferenceStatus {
	if in == nil {
		return nil
	}
	out := new(OptimizedInferenceStatus)
	in.DeepCopyInto(out)
	return out
}

func (in *OptimizedInferenceStatus) DeepCopyInto(out *OptimizedInferenceStatus) {
	*out = *in
	if in.LastOptimizedAt != nil {
		in, out := &in.LastOptimizedAt, &out.LastOptimizedAt
		*out = (*in).DeepCopy()
	}
}

func (in *OptimizedInferenceList) DeepCopy() *OptimizedInferenceList {
	if in == nil {
		return nil
	}
	out := new(OptimizedInferenceList)
	in.DeepCopyInto(out)
	return out
}

func (in *OptimizedInferenceList) DeepCopyInto(out *OptimizedInferenceList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]OptimizedInference, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// SchemeGroupVersion is group version used to register these objects
var SchemeGroupVersion = schema.GroupVersion{Group: "inferbolt.io", Version: "v1alpha1"}

// Kind takes an unqualified kind and returns back a Group qualified GroupKind
func Kind(kind string) schema.GroupKind {
	return SchemeGroupVersion.WithKind(kind).GroupKind()
}

// Resource takes an unqualified resource and returns a Group qualified GroupResource
func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

// AddToScheme adds the types in this group-version to the given scheme.
func AddToScheme(s *runtime.Scheme) error {
	s.AddKnownTypes(SchemeGroupVersion,
		&OptimizedInference{},
		&OptimizedInferenceList{},
	)
	metav1.AddToGroupVersion(s, SchemeGroupVersion)
	return nil
}