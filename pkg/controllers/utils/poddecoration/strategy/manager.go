/*
Copyright 2024 The KusionStack Authors.

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

package strategy

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/utils"
)

const (
	// syncedPollPeriod controls how often you look at the status of your sync funcs
	syncedPollPeriod = 200 * time.Millisecond
)

var SharedStrategyController Controller

var _ manager.Runnable = &strategyManager{}

type Controller interface {
	Updater
	Reader
	Start(context.Context) error
}

type Updater interface {
	// Synced indicates that all PodDecoration managers were updated.
	Synced()
	// UpdateSelectedPods is used to update the effective Pods of PodDecoration
	// to make real-time decisions about the PodDecoration version for each Pod.
	UpdateSelectedPods(context.Context, *appsv1alpha1.PodDecoration, []*corev1.Pod) error
	// DeletePodDecoration clean up invalid PodDecoration manager.
	DeletePodDecoration(*appsv1alpha1.PodDecoration)

	InjectClient(client.Client) error
}

type Reader interface {
	// WaitForSync waits for all PodDecoration managers cache were synced.
	WaitForSync(ctx context.Context) bool
	// LatestPodDecorations are a set of the most recent PodDecorations in the namespace.
	LatestPodDecorations(namespace string) []*appsv1alpha1.PodDecoration
	// EffectivePodRevisions is used to select the suitable version from the UpdatedRevision
	// and CurrentRevision among a set of the latest Decorations.
	EffectivePodRevisions(*corev1.Pod) (updatedRevisions, stableRevisions map[string]string)
}

func init() {
	SharedStrategyController = &strategyManager{
		managers: map[string]map[string]*podDecorationManager{},
	}
}

func (m *strategyManager) Start(ctx context.Context) error {
	allPodDecorations := &appsv1alpha1.PodDecorationList{}
	if err := m.List(ctx, allPodDecorations); err != nil {
		return err
	}
	q := workqueue.New()

	for i := range allPodDecorations.Items {
		pd := &allPodDecorations.Items[i]
		if pd.DeletionTimestamp != nil {
			continue
		}
		q.Add(types.NamespacedName{Namespace: pd.Namespace, Name: pd.Name})
	}
	defer m.Synced()
	for {
		select {
		case <-ctx.Done():
			klog.Warningf("PodDecoration manager runner shutdown")
			return nil
		default:
		}
		if q.Len() == 0 {
			break
		}
		item, _ := q.Get()
		q.Done(item)
		namespaceName := item.(types.NamespacedName)
		pd := &appsv1alpha1.PodDecoration{}
		if err := m.Get(ctx, namespaceName, pd); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			klog.Errorf("fail to get pod %s/%s, %v", namespaceName.Namespace, namespaceName.Name, err)
			q.Add(namespaceName)
		}
		if pd.Generation != pd.Status.ObservedGeneration {
			q.Add(namespaceName)
			klog.Infof("wait for PodDecoration %s/%s ObservedGeneration update", pd.Namespace, pd.Name)
			continue
		}
		podList := &corev1.PodList{}
		sel := labels.Everything()
		if pd.Spec.Selector != nil {
			sel, _ = metav1.LabelSelectorAsSelector(pd.Spec.Selector)
		}
		if err := m.List(ctx, podList, &client.ListOptions{Namespace: pd.Namespace, LabelSelector: sel}); err != nil {
			return err
		}
		var pods []*corev1.Pod
		for idx := range podList.Items {
			pods = append(pods, &podList.Items[idx])
		}
		if err := m.UpdateSelectedPods(ctx, pd, pods); err != nil {
			klog.Errorf("fail to update PodDecotation %s/%s strategy manager, %v", pd.Namespace, pd.Name, err)
		}
	}
	return nil
}

type strategyManager struct {
	client.Client
	// PDNamespace:PDName:Manager
	managers map[string]map[string]*podDecorationManager
	synced   bool
	mu       sync.RWMutex
}

func (m *strategyManager) HasSynced() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.synced
}

func (m *strategyManager) Synced() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.synced = true
}

func (m *strategyManager) InjectClient(c client.Client) error {
	m.Client = c
	return nil
}

func (m *strategyManager) WaitForSync(ctx context.Context) bool {
	err := wait.PollImmediateUntilWithContext(ctx, syncedPollPeriod,
		func(context.Context) (bool, error) {
			return m.HasSynced(), nil
		})
	if err != nil {
		klog.V(2).Infof("stop requested")
		return false
	}
	return true
}

func (m *strategyManager) LatestPodDecorations(namespace string) (pds []*appsv1alpha1.PodDecoration) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	namespacedMgr, ok := m.managers[namespace]
	if ok {
		for _, mgr := range namespacedMgr {
			pds = append(pds, mgr.latestPodDecoration.DeepCopy())
		}
	}
	return
}

func (m *strategyManager) EffectivePodRevisions(po *corev1.Pod) (updatedRevisions, stableRevisions map[string]string) {
	updatedRevisions, stableRevisions = map[string]string{}, map[string]string{}
	m.mu.RLock()
	namespacedMgr, ok := m.managers[po.Namespace]
	m.mu.RUnlock()
	if !ok {
		return
	}
	for pdName, mgr := range namespacedMgr {
		revision, isUpdated := mgr.getSuitableRevision(po)
		if revision == nil || *revision == "" {
			continue
		}
		if isUpdated {
			updatedRevisions[pdName] = *revision
		} else {
			stableRevisions[pdName] = *revision
		}
	}
	return
}

func (m *strategyManager) UpdateSelectedPods(ctx context.Context, pd *appsv1alpha1.PodDecoration, pods []*corev1.Pod) error {
	return m.podDecorationMgr(pd).updateSelectedPods(ctx, pd, pods)
}

func (m *strategyManager) DeletePodDecoration(pd *appsv1alpha1.PodDecoration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	namespacedMgr, ok := m.managers[pd.Namespace]
	if !ok {
		return
	}
	delete(namespacedMgr, pd.Name)
}

func (m *strategyManager) podDecorationMgr(pd *appsv1alpha1.PodDecoration) *podDecorationManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	namespacedManager, ok := m.managers[pd.Namespace]
	if !ok {
		m.managers[pd.Namespace] = make(map[string]*podDecorationManager)
	}
	pm, ok := namespacedManager[pd.Name]
	if ok {
		return pm
	}
	pm = &podDecorationManager{
		c:             m.Client,
		name:          pd.Name,
		namespace:     pd.Namespace,
		effectivePods: map[string]*podInfo{},
	}
	namespacedManager[pd.Name] = pm
	return pm
}

type podDecorationManager struct {
	c                        client.Client
	name, namespace          string
	effectivePods            effectivePods
	partitionOldRevisionPods sets.String
	latestPodDecoration      *appsv1alpha1.PodDecoration
	mu                       sync.RWMutex
}

func (pm *podDecorationManager) updateSelectedPods(ctx context.Context, pd *appsv1alpha1.PodDecoration, pods []*corev1.Pod) error {
	// Update strategy range,
	//   case 1: PodDecoration selector changed;
	//   case 2: Pod deleted, but instanceID exists.
	// This could be done in O(n*log(n)). n=len(pods).
	// Getter is used to cache related resources.
	getter := newPodRelatedResourceGetter(ctx, pm.c)
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.latestPodDecoration = pd.DeepCopy()
	newPods := map[string]*corev1.Pod{}
	newEffectivePods := map[string]*podInfo{}
	existInstanceId := sets.NewString()
	oldPods := pm.effectivePods
	for i, pod := range pods {
		if !IsActive(pod) {
			continue
		}
		newPods[pod.Name] = pods[i]
		if oldPod, ok := oldPods[pod.Name]; ok {
			newEffectivePods[pod.Name] = oldPod
		} else {
			newPodInfo, err := getter.buildPodInfo(pod, pd)
			if err != nil {
				return err
			}
			newEffectivePods[pod.Name] = newPodInfo
			existInstanceId.Insert(newPodInfo.InstanceKey())
		}
	}
	for podName, info := range oldPods {
		// Scaled, release placeholder
		if existInstanceId.Has(info.InstanceKey()) {
			continue
		}
		// PodDecoration selector changed
		if !utils.Selected(pm.latestPodDecoration.Spec.Selector, info.labels) {
			continue
		}
		_, ok := newEffectivePods[podName]
		if !ok {
			resource, err := getter.relatePodInfo(info)
			if err != nil {
				return err
			}
			// Placeholder case: pod deleted but instanceId exists.
			if getAllocatedId(resource.ResourceContext).Has(info.instanceId) {
				info.isDeleted = true
				newEffectivePods[podName] = info
			}
		}
	}
	pm.effectivePods = newEffectivePods
	if pm.latestPodDecoration.Spec.UpdateStrategy.RollingUpdate != nil ||
		pm.latestPodDecoration.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		pm.updatePartitionPods(pm.effectivePods, pm.latestPodDecoration.Status.UpdatedRevision,
			int(*pm.latestPodDecoration.Spec.UpdateStrategy.RollingUpdate.Partition))
	}
	// TODO: write UpdatedRevision in Context
	return nil
}

func (pm *podDecorationManager) needUpdate(pod *corev1.Pod) bool {

	return false
}

func (pm *podDecorationManager) getSuitableRevision(pod *corev1.Pod) (revision *string, isUpdated bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if !match(pm.latestPodDecoration.Spec.Selector, pod.Labels) {
		return nil, false
	}
	updateRev := pm.latestPodDecoration.Status.UpdatedRevision
	currentRev := pm.latestPodDecoration.Status.CurrentRevision
	// default nil select all
	if pm.latestPodDecoration.Spec.UpdateStrategy.RollingUpdate == nil {
		return &updateRev, true
	}
	// bu selector
	if pm.latestPodDecoration.Spec.UpdateStrategy.RollingUpdate.Selector != nil {
		if match(pm.latestPodDecoration.Spec.UpdateStrategy.RollingUpdate.Selector, pod.Labels) {
			return &updateRev, true
		}
		return &currentRev, false
	}
	// by partition
	if pm.latestPodDecoration.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		if pm.partitionOldRevisionPods.Has(pod.Name) {
			return &currentRev, false
		}
		return &updateRev, true
	}
	return &updateRev, true
}

func (pm *podDecorationManager) updatePartitionPods(pods effectivePods, revision string, partition int) {
	pm.partitionOldRevisionPods = sets.NewString()
	sortedPodInfos := &sortedPodInfo{revision: revision}
	for _, info := range pods {
		sortedPodInfos.infos = append(sortedPodInfos.infos, info)
	}
	sort.Sort(sortedPodInfos)

	for i := len(sortedPodInfos.infos) - partition; i < len(sortedPodInfos.infos); i++ {
		pm.partitionOldRevisionPods.Insert(sortedPodInfos.infos[i].name)
	}
}

func newPodRelatedResourceGetter(ctx context.Context, c client.Client) *podRelatedResourceGetter {
	return &podRelatedResourceGetter{
		ctx:    ctx,
		Client: c,
	}
}

// podRelatedResourceGetter
type podRelatedResourceGetter struct {
	ctx context.Context
	client.Client

	podResources      map[string]*relatedResource
	collaSetResources map[string]*relatedResource
}

type relatedResource struct {
	CollaSet        *appsv1alpha1.CollaSet
	ResourceContext *appsv1alpha1.ResourceContext
}

func (r *podRelatedResourceGetter) relatedPod(po *corev1.Pod) (*relatedResource, error) {
	if resource, ok := r.podResources[po.Name]; ok {
		return resource, nil
	}
	ownerRef := metav1.GetControllerOf(po)
	if ownerRef == nil || ownerRef.Kind != "CollaSet" {
		return nil, fmt.Errorf("pod %s was not controlled by collaset", po.Name)
	}

	if resource, ok := r.collaSetResources[ownerRef.Name]; ok {
		r.podResources[po.Name] = resource
		return resource, nil
	}

	resource, err := r.getResources(po.Namespace, ownerRef.Name)
	if err != nil {
		return nil, err
	}
	r.collaSetResources[ownerRef.Name] = resource
	r.podResources[po.Name] = resource
	return resource, nil
}

func (r *podRelatedResourceGetter) relatePodInfo(info *podInfo) (*relatedResource, error) {
	resource, ok := r.collaSetResources[info.collaSet]
	if ok {
		return resource, nil
	}
	resource, err := r.getResources(info.namespace, info.collaSet)
	if err != nil {
		return nil, err
	}
	r.collaSetResources[info.collaSet] = resource
	r.podResources[info.name] = resource
	return nil, nil
}

func (r *podRelatedResourceGetter) getResources(namespace, collaSetName string) (*relatedResource, error) {
	cls := &appsv1alpha1.CollaSet{}
	if err := r.Get(r.ctx, types.NamespacedName{Namespace: namespace, Name: collaSetName}, cls); err != nil {
		return nil, err
	}
	rc, err := GetResourceContext(r.ctx, r.Client, cls)
	if err != nil {
		return nil, err
	}
	resource := &relatedResource{
		CollaSet:        cls,
		ResourceContext: rc,
	}
	return resource, nil
}

func (r *podRelatedResourceGetter) buildPodInfo(pod *corev1.Pod, pd *appsv1alpha1.PodDecoration) (*podInfo, error) {
	resource, err := r.relatedPod(pod)
	if err != nil {
		return nil, err
	}
	info := &podInfo{
		name:            pod.Name,
		namespace:       pod.Namespace,
		labels:          pod.Labels,
		collaSet:        resource.CollaSet.Name,
		resourceContext: resource.ResourceContext.Name,
		instanceId:      pod.Labels[appsv1alpha1.PodInstanceIDLabelKey],
		revision:        pod.Labels[appsv1alpha1.PodDecorationLabelPrefix+pd.Name],
	}
	return info, nil
}

func match(selector *metav1.LabelSelector, lb map[string]string) bool {
	sel := labels.Everything()
	if sel != nil {
		sel, _ = metav1.LabelSelectorAsSelector(selector)
	}
	return sel.Matches(labels.Set(lb))
}
