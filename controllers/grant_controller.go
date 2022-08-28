/*
Copyright 2022.

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
	"context"
	"database/sql"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	crdbv1alpha1 "github.com/paulfarver/oggy/api/v1alpha1"
	"github.com/pkg/errors"
)

// GrantReconciler reconciles a Grant object
type GrantReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=crdb.oggy.haugland.io,resources=grants,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=crdb.oggy.haugland.io,resources=grants/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=crdb.oggy.haugland.io,resources=grants/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Grant object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile
func (r *GrantReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	grant := &crdbv1alpha1.Grant{}
	if err := r.Get(ctx, req.NamespacedName, grant); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to get grant")
	}

	dsn := "host=postgresql.oggy.haugland.io port=5432 user=postgres password=postgres dbname=postgres"

	dbClient, err := sql.Open("pgx", dsn)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "Failed to connect to database")
	}
	defer dbClient.Close()

	dbClient.ExecContext(ctx, "GRANT ")
	// TODO(user): your logic here

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GrantReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&crdbv1alpha1.Grant{}).
		Complete(r)
}
