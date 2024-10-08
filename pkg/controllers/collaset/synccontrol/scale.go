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

package synccontrol

import (
	"sort"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"

	collasetutils "kusionstack.io/kuperator/pkg/controllers/collaset/utils"
	"kusionstack.io/kuperator/pkg/controllers/utils/podopslifecycle"
)

func getPodsToDelete(filteredPods []*collasetutils.PodWrapper, replaceMapping map[string]*collasetutils.PodWrapper, diff int) []*collasetutils.PodWrapper {
	targetsPods := getTargetsDeletePods(filteredPods, replaceMapping)
	// 1. select pods to delete in first round according to diff
	sort.Sort(ActivePodsForDeletion(targetsPods))
	if diff > len(targetsPods) {
		diff = len(targetsPods)
	}

	var needDeletePods []*collasetutils.PodWrapper
	// 2. select pods to delete in second round according to replace, delete...
	for _, pod := range targetsPods[:diff] {
		if replacePairPod, exist := replaceMapping[pod.Name]; exist && replacePairPod != nil {
			// do not scaleIn new pod until origin pod is deleted, if you want to delete new pod, please delete it by label
			if replacePairPod.ToDelete {
				continue
			}
			// new pod not service available, just scaleIn it
			if _, serviceAvailable := replacePairPod.Labels[appsv1alpha1.PodServiceAvailableLabel]; !serviceAvailable {
				needDeletePods = append(needDeletePods, replacePairPod)
			}
		}
		needDeletePods = append(needDeletePods, pod)
	}

	return needDeletePods
}

// when sort pods to choose delete, only sort pods without replacement or these replace origin pods
func getTargetsDeletePods(filteredPods []*collasetutils.PodWrapper, replaceMapping map[string]*collasetutils.PodWrapper) []*collasetutils.PodWrapper {
	targetPods := make([]*collasetutils.PodWrapper, len(replaceMapping))
	index := 0
	for _, pod := range filteredPods {
		if _, exist := replaceMapping[pod.Name]; exist {
			targetPods[index] = pod
			index++
		}
	}

	return targetPods
}

type ActivePodsForDeletion []*collasetutils.PodWrapper

func (s ActivePodsForDeletion) Len() int      { return len(s) }
func (s ActivePodsForDeletion) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func (s ActivePodsForDeletion) Less(i, j int) bool {
	l, r := s[i], s[j]

	// pods which are indicated by ScaleStrategy.PodToDelete should be deleted before others
	if l.ToDelete != r.ToDelete {
		return l.ToDelete
	}

	// pods which are during scaleInOps should be deleted before those not during
	lDuringScaleIn := podopslifecycle.IsDuringOps(collasetutils.ScaleInOpsLifecycleAdapter, l)
	rDuringScaleIn := podopslifecycle.IsDuringOps(collasetutils.ScaleInOpsLifecycleAdapter, r)
	if lDuringScaleIn != rDuringScaleIn {
		return lDuringScaleIn
	}

	// TODO consider service available timestamps
	return collasetutils.ComparePod(l.Pod, r.Pod)
}
