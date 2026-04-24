/*
Copyright 2026.

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

package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	sonarqubev1alpha1 "github.com/BEIRDINH0S/sonarqube-operator/api/v1alpha1"
	"github.com/BEIRDINH0S/sonarqube-operator/internal/sonarqube"
)

const (
	annotationAdminInitialized = "sonarqube.io/admin-initialized"
	defaultAdminPassword       = "admin"
	requeueAfterHealthCheck    = 30 * time.Second
)

// SonarQubeInstanceReconciler reconciles a SonarQubeInstance object
type SonarQubeInstanceReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Recorder       record.EventRecorder
	NewSonarClient func(baseURL, token string) sonarqube.Client
}

// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubeinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubeinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubeinstances/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

func (r *SonarQubeInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	instance := &sonarqubev1alpha1.SonarQubeInstance{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling", "name", instance.Name, "phase", instance.Status.Phase)

	if err := r.reconcileStatefulSet(ctx, instance); err != nil {
		r.Recorder.Event(instance, corev1.EventTypeWarning, "StatefulSetFailed", err.Error())
		return ctrl.Result{}, fmt.Errorf("reconciling StatefulSet: %w", err)
	}

	if err := r.reconcileService(ctx, instance); err != nil {
		r.Recorder.Event(instance, corev1.EventTypeWarning, "ServiceFailed", err.Error())
		return ctrl.Result{}, fmt.Errorf("reconciling Service: %w", err)
	}

	if err := r.reconcileIngress(ctx, instance); err != nil {
		r.Recorder.Event(instance, corev1.EventTypeWarning, "IngressFailed", err.Error())
		return ctrl.Result{}, fmt.Errorf("reconciling Ingress: %w", err)
	}

	serviceURL := fmt.Sprintf("http://%s.%s:9000", instance.Name, instance.Namespace)
	result, err := r.reconcileHealth(ctx, instance, serviceURL)
	if err != nil {
		return ctrl.Result{}, err
	}

	instance.Status.Version = instance.Spec.Version
	instance.Status.URL = serviceURL
	if err := r.Status().Update(ctx, instance); err != nil {
		return ctrl.Result{}, fmt.Errorf("updating status: %w", err)
	}

	return result, nil
}

// reconcileHealth vérifie l'état de SonarQube et gère le premier démarrage.
// Retourne RequeueAfter si SonarQube n'est pas encore prêt.
func (r *SonarQubeInstanceReconciler) reconcileHealth(ctx context.Context, instance *sonarqubev1alpha1.SonarQubeInstance, serviceURL string) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	sonarClient := r.NewSonarClient(serviceURL, "")

	status, err := sonarClient.GetStatus(ctx)
	if err != nil {
		log.Info("SonarQube not reachable yet, requeueing", "error", err.Error())
		r.Recorder.Event(instance, corev1.EventTypeNormal, "Waiting", "Waiting for SonarQube to be reachable")
		instance.Status.Phase = "Progressing"
		return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
	}

	if status != "UP" {
		log.Info("SonarQube not ready yet", "status", status)
		r.Recorder.Event(instance, corev1.EventTypeNormal, "Waiting", fmt.Sprintf("SonarQube status: %s", status))
		instance.Status.Phase = "Progressing"
		return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
	}

	if instance.Annotations[annotationAdminInitialized] != "true" {
		if err := r.initializeAdminPassword(ctx, instance, sonarClient); err != nil {
			log.Error(err, "Failed to initialize admin password, will retry")
			instance.Status.Phase = "Progressing"
			return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
		}
	}

	instance.Status.Phase = "Ready"
	r.Recorder.Event(instance, corev1.EventTypeNormal, "Ready", "SonarQube instance is ready")
	return ctrl.Result{}, nil
}

// initializeAdminPassword change le mot de passe admin par défaut au premier démarrage
// et pose une annotation pour ne pas recommencer au prochain cycle.
func (r *SonarQubeInstanceReconciler) initializeAdminPassword(ctx context.Context, instance *sonarqubev1alpha1.SonarQubeInstance, sonarClient sonarqube.Client) error {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      instance.Spec.AdminSecretRef,
		Namespace: instance.Namespace,
	}, secret); err != nil {
		return fmt.Errorf("getting admin secret: %w", err)
	}

	newPassword := string(secret.Data["password"])
	if newPassword == "" {
		return fmt.Errorf("admin secret %q missing key 'password'", instance.Spec.AdminSecretRef)
	}

	if err := sonarClient.ChangeAdminPassword(ctx, defaultAdminPassword, newPassword); err != nil {
		return fmt.Errorf("changing admin password: %w", err)
	}

	if instance.Annotations == nil {
		instance.Annotations = map[string]string{}
	}
	instance.Annotations[annotationAdminInitialized] = "true"
	if err := r.Update(ctx, instance); err != nil {
		return fmt.Errorf("saving initialized annotation: %w", err)
	}

	r.Recorder.Event(instance, corev1.EventTypeNormal, "AdminInitialized", "Admin password initialized from secret")
	return nil
}

func (r *SonarQubeInstanceReconciler) reconcileStatefulSet(ctx context.Context, instance *sonarqubev1alpha1.SonarQubeInstance) error {
	desired := r.buildStatefulSet(instance)

	if err := controllerutil.SetControllerReference(instance, desired, r.Scheme); err != nil {
		return err
	}

	existing := &appsv1.StatefulSet{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if errors.IsNotFound(err) {
		r.Recorder.Event(instance, corev1.EventTypeNormal, "StatefulSetCreated", "StatefulSet created")
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	existing.Spec.Template.Spec.Containers[0].Image = desired.Spec.Template.Spec.Containers[0].Image
	existing.Spec.Template.Spec.Containers[0].Resources = desired.Spec.Template.Spec.Containers[0].Resources
	return r.Update(ctx, existing)
}

func (r *SonarQubeInstanceReconciler) reconcileService(ctx context.Context, instance *sonarqubev1alpha1.SonarQubeInstance) error {
	desired := r.buildService(instance)

	if err := controllerutil.SetControllerReference(instance, desired, r.Scheme); err != nil {
		return err
	}

	existing := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if errors.IsNotFound(err) {
		r.Recorder.Event(instance, corev1.EventTypeNormal, "ServiceCreated", "Service created")
		return r.Create(ctx, desired)
	}
	return err
}

func (r *SonarQubeInstanceReconciler) reconcileIngress(ctx context.Context, instance *sonarqubev1alpha1.SonarQubeInstance) error {
	if !instance.Spec.Ingress.Enabled {
		return nil
	}

	desired := r.buildIngress(instance)

	if err := controllerutil.SetControllerReference(instance, desired, r.Scheme); err != nil {
		return err
	}

	existing := &networkingv1.Ingress{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if errors.IsNotFound(err) {
		r.Recorder.Event(instance, corev1.EventTypeNormal, "IngressCreated", "Ingress created")
		return r.Create(ctx, desired)
	}
	return err
}

func (r *SonarQubeInstanceReconciler) buildStatefulSet(instance *sonarqubev1alpha1.SonarQubeInstance) *appsv1.StatefulSet {
	image := fmt.Sprintf("sonarqube:%s-%s", instance.Spec.Version, instance.Spec.Edition)
	labels := map[string]string{"app": "sonarqube", "instance": instance.Name}

	resources := instance.Spec.Resources
	if resources.Requests == nil {
		resources.Requests = corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("2Gi"),
			corev1.ResourceCPU:    resource.MustParse("500m"),
		}
	}

	storageSize := instance.Spec.Persistence.Size
	if storageSize == "" {
		storageSize = "10Gi"
	}

	replicas := int32(1)

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:      "sonarqube",
							Image:     image,
							Resources: resources,
							Ports: []corev1.ContainerPort{
								{Name: "http", ContainerPort: 9000, Protocol: corev1.ProtocolTCP},
							},
							Env: []corev1.EnvVar{
								{
									Name: "SONAR_JDBC_URL",
									Value: fmt.Sprintf("jdbc:postgresql://%s:%d/%s",
										instance.Spec.Database.Host,
										instance.Spec.Database.Port,
										instance.Spec.Database.Name,
									),
								},
								{
									Name: "SONAR_JDBC_USERNAME",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: instance.Spec.Database.SecretRef},
											Key:                  "username",
										},
									},
								},
								{
									Name: "SONAR_JDBC_PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: instance.Spec.Database.SecretRef},
											Key:                  "password",
										},
									},
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/api/system/status",
										Port: intstr.FromInt32(9000),
									},
								},
								InitialDelaySeconds: 60,
								PeriodSeconds:       10,
								FailureThreshold:    10,
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "data", MountPath: "/opt/sonarqube/data"},
							},
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "data"},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse(storageSize),
							},
						},
					},
				},
			},
		},
	}
}

func (r *SonarQubeInstanceReconciler) buildService(instance *sonarqubev1alpha1.SonarQubeInstance) *corev1.Service {
	labels := map[string]string{"app": "sonarqube", "instance": instance.Name}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 9000, TargetPort: intstr.FromInt32(9000), Protocol: corev1.ProtocolTCP},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
}

func (r *SonarQubeInstanceReconciler) buildIngress(instance *sonarqubev1alpha1.SonarQubeInstance) *networkingv1.Ingress {
	pathType := networkingv1.PathTypePrefix
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: instance.Spec.Ingress.Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: instance.Name,
											Port: networkingv1.ServiceBackendPort{Number: 9000},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *SonarQubeInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&sonarqubev1alpha1.SonarQubeInstance{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Named("sonarqubeinstance").
		Complete(r)
}
