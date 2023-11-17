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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

type podToPodDecorationMapper struct {
	client.Client
}

func (m *podToPodDecorationMapper) process(podObject client.Object) []reconcile.Request {
	pdList := &appsv1alpha1.PodDecorationList{}
	if err := m.List(context.TODO(), pdList, client.InNamespace(podObject.GetNamespace())); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, pd := range pdList.Items {
		selector, err := metav1.LabelSelectorAsSelector(pd.Spec.Selector)
		if err != nil {
			continue
		}

		if selector.Matches(labels.Set(podObject.GetLabels())) {
			requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: podObject.GetNamespace(), Name: podObject.GetName()}})
		}
	}

	return requests
}
