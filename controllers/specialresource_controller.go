/*


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

package controllers

import (
	"fmt"

	"github.com/go-logr/logr"
	srov1beta1 "github.com/openshift-psap/special-resource-operator/api/v1beta1"
	"github.com/openshift-psap/special-resource-operator/pkg/conditions"
	configv1 "github.com/openshift/api/config/v1"
	clientconfigv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	errs "github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	log logr.Logger
)

// SpecialResourceReconciler reconciles a SpecialResource object
type SpecialResourceReconciler struct {
	client.Client
	kubernetes.Clientset
	clientconfigv1.ConfigV1Client

	Log    logr.Logger
	Scheme *runtime.Scheme

	specialresource srov1beta1.SpecialResource
	parent          srov1beta1.SpecialResource
	dependency      srov1beta1.SpecialResourceDependency
	clusterOperator configv1.ClusterOperator
}

// Reconcile Reconiliation entry point
func (r *SpecialResourceReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {

	var err error
	var res reconcile.Result

	conds := conditions.NotAvailableProgressingNotDegraded(
		"Reconciling "+req.Name,
		"Reconciling "+req.Name,
		conditions.DegradedDefaultMsg,
	)
	// Do some preflight checks and get the cluster upgrade info
	if res, err = SpecialResourceUpgrade(r, req); err != nil {
		return res, errs.Wrap(err, "Cannot upgrade special resource")
	}
	// A resource is being reconciled set status to not available and only
	// if the reconcilation succeeds we're updating the conditions
	if res, err = SpecialResourcesStatus(r, req, conds); err != nil {
		return res, errs.Wrap(err, "Cannot update special resource status")
	}
	//Reconcile all specialresources
	if res, err = SpecialResourcesReconcile(r, req); err == nil && !res.Requeue {
		conds = conditions.AvailableNotProgressingNotDegraded()
	} else {
		return res, err
	}

	// Only if we're successfull we're going to update the status to
	// Available otherwise retunr the recondile error
	if res, err = SpecialResourcesStatus(r, req, conds); err != nil {
		log.Info("Cannot update special resource status", "error", fmt.Sprintf("%v", err))
		return res, nil
	}

	return reconcile.Result{}, nil
}

// SetupWithManager main initalization for manager
func (r *SpecialResourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&srov1beta1.SpecialResource{}).
		Owns(&v1.Pod{}).
		Owns(&appsv1.DaemonSet{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 1,
		}).
		Complete(r)
}
