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

	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/utils"
	"kusionstack.io/operating/pkg/utils/mixin"
)

var _ inject.Client = &ValidatingHandler{}
var _ admission.DecoderInjector = &ValidatingHandler{}

type ValidatingHandler struct {
	*mixin.WebhookHandlerMixin
}

func NewValidatingHandler() *ValidatingHandler {
	return &ValidatingHandler{
		WebhookHandlerMixin: mixin.NewWebhookHandlerMixin(),
	}
}

func (h *ValidatingHandler) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {

	// check group weight

	return admission.Allowed("")
}

func (h *ValidatingHandler) validateCreate(newPd *appsv1alpha1.PodDecoration) error {
	if newPd.Spec.InjectStrategy.Group == "" {
		return fmt.Errorf("PodDecoration %s InjectStrategy.Group can not be empty", utils.ObjectKeyString(newPd))
	}
	//pdList := &appsv1alpha1.PodDecorationList{}
	//if err := h.Client.List(context.TODO(), pdList, &client.ListOptions{Namespace: newPd.Namespace}); err != nil {
	//	return err
	//}
	//for _, pd := range pdList.Items {
	//	if pd.Spec.InjectStrategy.Group != newPd.Spec.InjectStrategy.Group {
	//		continue
	//	}
	//	if newPd.Spec.InjectStrategy.Weight == nil {
	//		return fmt.Errorf("PodDecoration %s InjectStrategy.Weight can not be nil", utils.ObjectKeyString(newPd))
	//	}
	//}
	return nil
}
