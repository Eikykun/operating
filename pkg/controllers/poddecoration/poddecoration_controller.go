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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/utils/expectations"
	utilspoddecoration "kusionstack.io/operating/pkg/controllers/utils/poddecoration"
	"kusionstack.io/operating/pkg/controllers/utils/revision"
	"kusionstack.io/operating/pkg/utils"
)

// Add creates a new PodDecoration Controller and adds it to the Manager with default RBAC.
// The Manager will set fields on the Controller and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcilePodDecoration{
		Client:          mgr.GetClient(),
		revisionManager: revision.NewRevisionManager(mgr.GetClient(), mgr.GetScheme(), &revisionOwnerAdapter{}),
	}
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
var (
	statusUpToDateExpectation = expectations.NewResourceVersionExpectation()
)

// ReconcilePodDecoration reconciles a PodDecoration object
type ReconcilePodDecoration struct {
	client.Client
	revisionManager *revision.RevisionManager
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
	if err := r.Get(context.TODO(), request.NamespacedName, instance); err != nil {
		// Object not found, return.  Created objects are automatically garbage collected.
		// For additional cleanup logic use finalizers.
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	key := utils.ObjectKeyString(instance)
	if !statusUpToDateExpectation.SatisfiedExpectations(key, instance.ResourceVersion) {
		klog.Infof("PodDecoration %s is not satisfied with updated status, requeue after", key)
		return reconcile.Result{Requeue: true}, nil
	}

	currentRevision, updatedRevision, _, collisionCount, _, err := r.revisionManager.ConstructRevisions(instance, false)
	if err != nil {
		return reconcile.Result{}, err
	}

	affectedPods, affectedCollaSets, err := r.filterOutPodAndCollaSet(ctx, instance)
	if err != nil {
		return reconcile.Result{}, err
	}
	newStatus := &appsv1alpha1.PodDecorationStatus{
		ObservedGeneration: instance.Generation,
		CurrentRevision:    currentRevision.Name,
		UpdatedRevision:    updatedRevision.Name,
		CollisionCount:     collisionCount,
	}
	err = r.calculateStatus(ctx, instance, newStatus, affectedPods, affectedCollaSets)
	if err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, r.updateStatus(ctx, instance, newStatus)
}

func (r *ReconcilePodDecoration) calculateStatus(
	ctx context.Context,
	instance *appsv1alpha1.PodDecoration,
	status *appsv1alpha1.PodDecorationStatus,
	affectedPods map[string][]*corev1.Pod,
	affectedCollaSets []*appsv1alpha1.CollaSet) error {

	heaviest, err := utilspoddecoration.GetHeaviestPDByGroup(ctx, r.Client, instance.Spec.InjectionStrategy.Group)
	if err != nil {
		return err
	}
	status.IsEffective = BoolPoint(heaviest == nil ||
		heaviest.Spec.InjectionStrategy.Group == heaviest.Spec.InjectionStrategy.Group)

	// TODO calculateStatus

	return nil
}

func (r *ReconcilePodDecoration) updateStatus(
	ctx context.Context,
	instance *appsv1alpha1.PodDecoration,
	status *appsv1alpha1.PodDecorationStatus) error {
	if equality.Semantic.DeepEqual(instance.Status, status) {
		return nil
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		instance.Status = *status
		updateErr := r.Status().Update(ctx, instance)
		if updateErr == nil {
			return nil
		}
		if err := r.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: instance.Name}, instance); err != nil {
			return fmt.Errorf("error getting PodDecoration %s: %v", utils.ObjectKeyString(instance), err)
		}
		return updateErr
	})
}

func (r *ReconcilePodDecoration) filterOutPodAndCollaSet(
	ctx context.Context,
	instance *appsv1alpha1.PodDecoration) (
	affectedPods map[string][]*corev1.Pod,
	affectedCollaSets []*appsv1alpha1.CollaSet, err error) {
	var sel labels.Selector
	podList := &corev1.PodList{}
	if instance.Spec.Selector != nil {
		sel, err = metav1.LabelSelectorAsSelector(instance.Spec.Selector)
	}
	if err = r.List(ctx, podList, &client.ListOptions{
		Namespace:     instance.Namespace,
		LabelSelector: sel,
	}); err != nil || len(podList.Items) == 0 {
		return
	}
	affectedPods = map[string][]*corev1.Pod{}
	for i := 0; i < len(podList.Items); i++ {
		ownerRef := metav1.GetControllerOf(&podList.Items[i])
		if ownerRef != nil && ownerRef.Kind == "CollaSet" {
			affectedPods[ownerRef.Name] = append(affectedPods[ownerRef.Name], &podList.Items[i])
		}
	}
	affectedCollaSets = make([]*appsv1alpha1.CollaSet, 0, len(affectedPods))
	for name := range affectedPods {
		colla := &appsv1alpha1.CollaSet{}
		if err = r.Get(ctx, types.NamespacedName{Name: name, Namespace: instance.Namespace}, colla); err != nil {
			return
		}
		affectedCollaSets = append(affectedCollaSets, colla)
	}
	return
}

func BoolPoint(val bool) *bool {
	return &val
}
