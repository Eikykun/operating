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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

// Add creates a new PodDecoration Controller and adds it to the Manager with default RBAC.
// The Manager will set fields on the Controller and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcilePodDecoration{Client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("poddecoration-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to PodDecoration
	err = c.Watch(&source.Kind{Type: &appsv1alpha1.PodDecoration{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}
	managerClient := mgr.GetClient()
	// Watch update of Pods which can be selected by PodDecoration
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, handler.EnqueueRequestsFromMapFunc(func(podObject client.Object) []reconcile.Request {
		pdList := &appsv1alpha1.PodDecorationList{}
		if listErr := managerClient.List(context.TODO(), pdList, client.InNamespace(podObject.GetNamespace())); listErr != nil {
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
	}))
	return err
}

var _ reconcile.Reconciler = &ReconcilePodDecoration{}

// ReconcilePodDecoration reconciles a PodDecoration object
type ReconcilePodDecoration struct {
	client.Client
	scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=poddecorations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=poddecorations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=poddecorations/finalizers,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=collasets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=collasets/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=controllerrevisions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;update;patch

// Reconcile reads that state of the cluster for a PodDecoration object and makes changes based on the state read
// and what is in the PodDecoration.Spec
func (r *ReconcilePodDecoration) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	// Fetch the PodDecoration instance
	instance := &appsv1alpha1.PodDecoration{}
	err := r.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *ReconcilePodDecoration) filterOutPodAndCollaSet(ctx context.Context, instance *appsv1alpha1.PodDecoration) ([]*corev1.Pod, []*appsv1alpha1.CollaSet, error) {
	return nil, nil, nil
}
