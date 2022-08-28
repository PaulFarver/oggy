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
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"

	_ "github.com/jackc/pgx/v4/stdlib"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/jackc/pgx/v5"
	crdbv1alpha1 "github.com/paulfarver/oggy/api/v1alpha1"
	"github.com/pkg/errors"
)

// DatabaseReconciler reconciles a Database object
type DatabaseReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=crdb.oggy.haugland.io,resources=databases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=crdb.oggy.haugland.io,resources=databases/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=crdb.oggy.haugland.io,resources=databases/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Database object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile
func (r *DatabaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Database object
	logger.Info("Reconciling Database")
	var db *crdbv1alpha1.Database
	if err := r.Get(ctx, req.NamespacedName, db); err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "Failed to fetch database %s/%s", req.Namespace, req.Name)
	}
	if db == nil {
		return ctrl.Result{}, errors.New("Database fetched was unexpectedly nil")
	}

	// Fetch cluster object referenced by Database
	var cluster *crdbv1alpha1.Cluster
	if err := r.Get(ctx, client.ObjectKey{Namespace: db.Spec.Cluster.Namespace, Name: db.Spec.Cluster.Name}, cluster); err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "Failed to fetch cluster '%s/%s' referenced by database '%s/%s'", db.Spec.Cluster.Namespace, db.Spec.Cluster.Name, db.Namespace, db.Name)
	}
	if cluster == nil {
		return ctrl.Result{}, errors.New("cluster fetched was unexpectedly nil")
	}

	config, err := r.connectConfigFromCluster(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "Failed to connect to cluster '%s/%s'", cluster.Namespace, cluster.Name)
	}

	// Connect to the database
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "Failed to connect to cluster '%s/%s'", cluster.Namespace, cluster.Name)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, "CREATE DATABASE IF NOT EXISTS ? WITH ENCODING = ?", db.Name, db.Spec.Encoding); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "Failed to create database")
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&crdbv1alpha1.Database{}).
		Complete(r)
}

func (r *DatabaseReconciler) getPassword(ctx context.Context, cluster *crdbv1alpha1.Cluster) (string, error) {
	var sec *corev1.Secret
	a := cluster.Spec.Authentication
	if err := r.Get(ctx, client.ObjectKey{Name: a.Password.Name, Namespace: a.Password.Namespace}, sec); err != nil {
		return "", errors.Wrapf(err, "Failed to fetch password secret '%s/%s' for cluster '%s/%s'", a.Password.Namespace, a.Password.Name, cluster.Namespace, cluster.Name)
	}
	if sec == nil {
		return "", errors.New("secret fetched was unexpectedly nil")
	}

	if d, ok := sec.Data[a.Password.Key]; ok {
		pwd := []byte{}
		_, err := base64.StdEncoding.Decode(pwd, d)
		if err != nil {
			return "", errors.Wrapf(err, "Failed to decode password secret '%s/%s' for cluster '%s/%s'", a.Password.Namespace, a.Password.Name, cluster.Namespace, cluster.Name)
		}
		return string(pwd), nil
	}
	return "", errors.Errorf("password secret did not contain key %s", a.Password.Key)
}

func (r *DatabaseReconciler) getTLS(ctx context.Context, cluster *crdbv1alpha1.Cluster) (*x509.CertPool, tls.Certificate, error) {
	var sec *corev1.Secret
	pool := x509.NewCertPool()
	var certificate tls.Certificate
	if err := r.Get(ctx, client.ObjectKey{Name: cluster.Spec.Authentication.TLS.Name, Namespace: cluster.Spec.Authentication.TLS.Name}, sec); err != nil {
		return pool, certificate, errors.Wrapf(err, "Failed to fetch TLS secret '%s/%s' for cluster '%s/%s'", cluster.Spec.Authentication.TLS.Namespace, cluster.Spec.Authentication.TLS.Name, cluster.Namespace, cluster.Name)
	}
	if sec == nil {
		return pool, certificate, errors.New("secret fetched was unexpectedly nil")
	}

	if d, ok := sec.Data["ca.crt"]; ok {
		ca := []byte{}
		_, err := base64.StdEncoding.Decode(ca, d)
		if err != nil {
			return pool, certificate, errors.Wrapf(err, "Failed to decode TLS secret '%s/%s' for cluster '%s/%s'", cluster.Spec.Authentication.TLS.Namespace, cluster.Spec.Authentication.TLS.Name, cluster.Namespace, cluster.Name)
		}

		if !pool.AppendCertsFromPEM(ca) {
			return pool, certificate, errors.New("Failed to append CA certificate to pool")
		}
	}

	clientCert := []byte{}
	if d, ok := sec.Data["tls.crt"]; ok {
		_, err := base64.StdEncoding.Decode(clientCert, d)
		if err != nil {
			return pool, certificate, errors.Wrapf(err, "Failed to decode TLS secret '%s/%s' for cluster '%s/%s'", cluster.Spec.Authentication.TLS.Namespace, cluster.Spec.Authentication.TLS.Name, cluster.Namespace, cluster.Name)
		}
	} else {
		return pool, certificate, errors.New("TLS secret did not contain key 'tls.crt'")
	}

	clientKey := []byte{}
	if d, ok := sec.Data["tls.key"]; ok {
		_, err := base64.StdEncoding.Decode(clientKey, d)
		if err != nil {
			return pool, certificate, errors.Wrapf(err, "Failed to decode TLS secret '%s/%s' for cluster '%s/%s'", cluster.Spec.Authentication.TLS.Namespace, cluster.Spec.Authentication.TLS.Name, cluster.Namespace, cluster.Name)
		}
	} else {
		return pool, certificate, errors.New("TLS secret did not contain key 'tls.key'")
	}

	certificate, err := tls.X509KeyPair(clientCert, clientKey)
	if err != nil {
		return pool, certificate, errors.Wrapf(err, "Failed to create TLS key pair for cluster '%s/%s'", cluster.Namespace, cluster.Name)
	}

	return pool, certificate, nil
}

func (r *DatabaseReconciler) connectConfigFromCluster(ctx context.Context, cluster *crdbv1alpha1.Cluster) (*pgx.ConnConfig, error) {
	connString := fmt.Sprintf("host=%s port=%d user=%s sslmode=%s application_name=oggy", cluster.Spec.Host, cluster.Spec.Port, cluster.Spec.Authentication.Username, cluster.Spec.Authentication.TLS.Mode)
	if (cluster.Spec.Authentication.Password != crdbv1alpha1.PasswordSpec{}) {
		pwd, err := r.getPassword(ctx, cluster)
		if err == nil {
			return nil, errors.Wrapf(err, "Failed to get password for cluster '%s/%s'", cluster.Namespace, cluster.Name)
		}
		connString += fmt.Sprintf(" password='%s'", pwd)
	}

	config, err := pgx.ParseConfig(connString)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to parse connection string for cluster '%s/%s'", cluster.Namespace, cluster.Name)
	}

	if (cluster.Spec.Authentication.TLS != crdbv1alpha1.TLSSpec{}) {
		caPool, certificate, err := r.getTLS(ctx, cluster)
		if err != nil {
			return nil, errors.Wrapf(err, "Failed to get TLS for cluster '%s/%s'", cluster.Namespace, cluster.Name)
		}
		config.TLSConfig.Certificates = []tls.Certificate{certificate}
		config.TLSConfig.RootCAs = caPool
	}

	return config, nil
}
