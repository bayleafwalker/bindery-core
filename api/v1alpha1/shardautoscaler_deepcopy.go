package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto copies the receiver, writing into out. in must be non-nil.
func (in *ShardAutoscaler) DeepCopyInto(out *ShardAutoscaler) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy copies the receiver, creating a new ShardAutoscaler.
func (in *ShardAutoscaler) DeepCopy() *ShardAutoscaler {
	if in == nil {
		return nil
	}
	out := new(ShardAutoscaler)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject copies the receiver, creating a new runtime.Object.
func (in *ShardAutoscaler) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto copies the receiver, writing into out. in must be non-nil.
func (in *ShardAutoscalerList) DeepCopyInto(out *ShardAutoscalerList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		out.Items = make([]ShardAutoscaler, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopy copies the receiver, creating a new ShardAutoscalerList.
func (in *ShardAutoscalerList) DeepCopy() *ShardAutoscalerList {
	if in == nil {
		return nil
	}
	out := new(ShardAutoscalerList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject copies the receiver, creating a new runtime.Object.
func (in *ShardAutoscalerList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto copies the receiver, writing into out. in must be non-nil.
func (in *ShardAutoscalerSpec) DeepCopyInto(out *ShardAutoscalerSpec) {
	*out = *in
	out.WorldRef = in.WorldRef
	if in.Metrics != nil {
		out.Metrics = make([]MetricSpec, len(in.Metrics))
		for i := range in.Metrics {
			in.Metrics[i].DeepCopyInto(&out.Metrics[i])
		}
	}
}

// DeepCopyInto copies the receiver, writing into out. in must be non-nil.
func (in *MetricSpec) DeepCopyInto(out *MetricSpec) {
	*out = *in
	if in.Resource != nil {
		in, out := &in.Resource, &out.Resource
		*out = new(ResourceMetricSource)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopyInto copies the receiver, writing into out. in must be non-nil.
func (in *ResourceMetricSource) DeepCopyInto(out *ResourceMetricSource) {
	*out = *in
	if in.TargetAverageUtilization != nil {
		in, out := &in.TargetAverageUtilization, &out.TargetAverageUtilization
		*out = new(int32)
		**out = **in
	}
}

// DeepCopyInto copies the receiver, writing into out. in must be non-nil.
func (in *ShardAutoscalerStatus) DeepCopyInto(out *ShardAutoscalerStatus) {
	*out = *in
	if in.LastScaleTime != nil {
		in, out := &in.LastScaleTime, &out.LastScaleTime
		*out = (*in).DeepCopy()
	}
	if in.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(in.Conditions))
		for i := range in.Conditions {
			in.Conditions[i].DeepCopyInto(&out.Conditions[i])
		}
	}
}
