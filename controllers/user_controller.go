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
	"crypto/rand"
	"database/sql"
	"encoding/base64"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	crdbv1alpha1 "github.com/paulfarver/oggy/api/v1alpha1"
	"github.com/pkg/errors"
)

// UserReconciler reconciles a User object
type UserReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=crdb.oggy.haugland.io,resources=users,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=crdb.oggy.haugland.io,resources=users/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=crdb.oggy.haugland.io,resources=users/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the User object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile
func (r *UserReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	user := &crdbv1alpha1.User{}
	if err := r.Get(ctx, req.NamespacedName, user); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to get user")
	}

	passwordSecret := &corev1.Secret{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: user.Namespace, Name: user.Spec.PasswordSecretRef}, passwordSecret); err != nil {
		// Generate a new password and store it in the secret.
		passwordSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      user.Spec.PasswordSecretRef,
				Namespace: user.Namespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"password": []byte(base64.StdEncoding.EncodeToString([]byte(GeneratePassword()))),
			},
		}
		if err := r.Create(ctx, passwordSecret); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to create password secret")
		}
	}

	byt, err := base64.StdEncoding.DecodeString(string(passwordSecret.Data["password"]))
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to decode password secret")
	}

	dsn := "host=postgresql.oggy.haugland.io port=5432 user=postgres password=postgres dbname=postgres"

	dbClient, err := sql.Open("pgx", dsn)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "Failed to connect to database")
	}
	defer dbClient.Close()

	if _, err := dbClient.ExecContext(ctx, "CREATE USER IF NOT EXISTS ? WITH PASSWORD null", user.Spec.UserName); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "Failed to create user")
	}

	if _, err := dbClient.ExecContext(ctx, "ALTER USER ? WITH PASSWORD ?", user.Spec.UserName, string(byt)); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "Failed to set password")
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *UserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&crdbv1alpha1.User{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}

func GeneratePassword() string {
	bs := make([]byte, 64)
	_, err := rand.Read(bs)
	if err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(bs)
}
