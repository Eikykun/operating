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
	"context"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

func GetEffectiveDecorationsByCollaSet(
	ctx context.Context,
	c client.Client,
	colla *appsv1alpha1.CollaSet) (
	podDecorations []*appsv1alpha1.PodDecoration, err error) {
	pdList := &appsv1alpha1.PodDecorationList{}
	if err = c.List(ctx, pdList, &client.ListOptions{Namespace: colla.Namespace}); err != nil {
		return
	}
	for i := range pdList.Items {
		if isAffectedCollaSet(&pdList.Items[i], colla) {
			podDecorations = append(podDecorations, &pdList.Items[i])
		}
	}
	podDecorations = PickGroupTop(podDecorations)
	return
}

func GetPodEffectiveDecorations(pod *corev1.Pod, podDecorations []*appsv1alpha1.PodDecoration) (res []*appsv1alpha1.PodDecoration) {
	for i, pd := range podDecorations {
		if pd.Spec.Selector != nil {
			sel, _ := metav1.LabelSelectorAsSelector(pd.Spec.Selector)
			if !sel.Matches(labels.Set(pod.Labels)) {
				continue
			}
		}
		// no rolling upgrade, upgrade all
		if pd.Spec.UpdateStrategy.RollingUpdate == nil {
			res = append(res, podDecorations[i])
			continue
		}
		// by selector
		if pd.Spec.UpdateStrategy.RollingUpdate.Selector != nil {
			sel, _ := metav1.LabelSelectorAsSelector(pd.Spec.UpdateStrategy.RollingUpdate.Selector)
			if sel.Matches(labels.Set(pod.Labels)) {
				res = append(res, podDecorations[i])
			}
			continue
		}
		// TODO: by partition
		//if pd.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		//}
	}
	return
}

func PickGroupTop(podDecorations []*appsv1alpha1.PodDecoration) (res []*appsv1alpha1.PodDecoration) {
	sort.Sort(PodDecorations(podDecorations))
	for i, pd := range podDecorations {
		if i == 0 {
			res = append(res, podDecorations[i])
			continue
		}
		if pd.Spec.InjectionStrategy.Group == res[len(res)-1].Spec.InjectionStrategy.Group {
			continue
		}
		res = append(res, podDecorations[i])
	}
	return
}

func isAffectedCollaSet(pd *appsv1alpha1.PodDecoration, colla *appsv1alpha1.CollaSet) bool {
	for _, detail := range pd.Status.Details {
		if detail.CollaSet == colla.Name {
			return true
		}
	}
	return false
}
