/*
Copyright 2023 The KusionStack Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package poddecoration

import (
	corev1 "k8s.io/api/core/v1"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

func PatchPodDecoration(pod *corev1.Pod, template *appsv1alpha1.PodDecorationPodTemplate) (err error) {
	if len(template.Metadata) > 0 {
		if _, err = PatchMetadata(&pod.ObjectMeta, template.Metadata); err != nil {
			return
		}
	}
	if len(template.InitContainers) > 0 {
		AddInitContainers(pod, template.InitContainers)
	}

	if len(template.PrimaryContainers) > 0 {
		PrimaryContainerPatch(pod, template.PrimaryContainers)
	}

	if len(template.Containers) > 0 {
		ContainersPatch(pod, template.Containers)
	}

	if len(template.Volumes) > 0 {
		pod.Spec.Volumes = MergeVolumes(pod.Spec.Volumes, template.Volumes)
	}

	if template.Affinity != nil {
		PatchAffinity(pod, template.Affinity)
	}

	if template.Tolerations != nil {
		pod.Spec.Tolerations = MergeTolerations(pod.Spec.Tolerations, template.Tolerations)
	}
	return nil
}
