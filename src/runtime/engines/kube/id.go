package kube

import (
	"fmt"

	k8s "k8s.io/kubernetes/pkg/kubelet/apis/cri/runtime/v1alpha2"
)

// ContainerID returns container id based on passed podID and container metadata.
func ContainerID(podID string, meta *k8s.ContainerMetadata) string {
	return fmt.Sprintf("%s_%d", meta.GetName(), meta.GetAttempt())
}

// PodID returns pod id based on pod metadata.
func PodID(meta *k8s.PodSandboxMetadata) string {
	return fmt.Sprintf("%s_%s_%s_%d", meta.GetName(), meta.GetNamespace(), meta.GetUid(), meta.GetAttempt())
}
