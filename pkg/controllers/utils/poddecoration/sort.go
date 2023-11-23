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

	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/utils/inject"
)

type PodDecorations []*appsv1alpha1.PodDecoration

func (br PodDecorations) Len() int {
	return len(br)
}

func (br PodDecorations) Less(i, j int) bool {
	if br[i].Spec.InjectionStrategy.Group == br[j].Spec.InjectionStrategy.Group {
		if *br[i].Spec.InjectionStrategy.Weight == *br[j].Spec.InjectionStrategy.Weight {
			br[i].CreationTimestamp.After(br[j].CreationTimestamp.Time)
		}
		return *br[i].Spec.InjectionStrategy.Weight > *br[j].Spec.InjectionStrategy.Weight
	}
	return br[i].Spec.InjectionStrategy.Group < br[j].Spec.InjectionStrategy.Group
}

func (br PodDecorations) Swap(i, j int) {
	br[i], br[j] = br[j], br[i]
}

func BuildSortedPodDecorationPointList(list *appsv1alpha1.PodDecorationList) []*appsv1alpha1.PodDecoration {
	res := PodDecorations{}
	for i := range list.Items {
		res = append(res, &list.Items[i])
	}
	sort.Sort(res)
	return res
}

func GetHeaviestPDByGroup(ctx context.Context, c client.Client, group string) (heaviest *appsv1alpha1.PodDecoration, err error) {
	pdList := &appsv1alpha1.PodDecorationList{}
	if err = c.List(ctx, pdList,
		&client.ListOptions{FieldSelector: fields.OneTermEqualSelector(
			inject.FieldIndexPodDecorationGroup, group)}); err != nil {
		return
	}
	podDecorations := BuildSortedPodDecorationPointList(pdList)
	if len(podDecorations) > 0 {
		return podDecorations[0], nil
	}
	return
}
