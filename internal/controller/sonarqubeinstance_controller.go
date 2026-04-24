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
	defaultAdminPassword    = "admin"
	requeueAfterHealthCheck = 30 * time.Second
)

// SonarQubeInstanceReconciler reconciles a SonarQubeInstance object
type SonarQubeInstanceReconciler struct {
	client.Client
	Scheme                     *runtime.Scheme
	Recorder                   record.EventRecorder
	NewSonarClient             func(baseURL, token string) sonarqube.Client
	NewSonarClientWithPassword func(baseURL, username, password string) sonarqube.Client
}

// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubeinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubeinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sonarqube.sonarqube.io,resources=sonarqubeinstances/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
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

	if err := r.reconcileHeadlessService(ctx, instance); err != nil {
		r.Recorder.Event(instance, corev1.EventTypeWarning, "HeadlessServiceFailed", err.Error())
		return ctrl.Result{}, fmt.Errorf("reconciling headless Service: %w", err)
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

	// GetStatus est un endpoint public — pas besoin de token
	unauthClient := r.NewSonarClient(serviceURL, "")
	status, err := unauthClient.GetStatus(ctx)
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

	if err := r.initializeAdmin(ctx, instance, serviceURL); err != nil {
		log.Error(err, "Failed to initialize admin, will retry")
		instance.Status.Phase = "Progressing"
		return ctrl.Result{RequeueAfter: requeueAfterHealthCheck}, nil
	}

	instance.Status.Phase = "Ready"
	r.Recorder.Event(instance, corev1.EventTypeNormal, "Ready", "SonarQube instance is ready")
	return ctrl.Result{}, nil
}

// adminTokenSecretName retourne le nom du Secret qui stocke le token Bearer de l'admin.
func adminTokenSecretName(instance *sonarqubev1alpha1.SonarQubeInstance) string {
	return instance.Name + "-admin-token"
}

// initializeAdmin gère l'initialisation idempotente du compte admin :
// - si le Secret token existe déjà → rien à faire
// - sinon : vérifie quel password fonctionne, change si nécessaire, génère un token Bearer
func (r *SonarQubeInstanceReconciler) initializeAdmin(ctx context.Context, instance *sonarqubev1alpha1.SonarQubeInstance, serviceURL string) error {
	tokenSecretName := adminTokenSecretName(instance)

	// Si le Secret token existe déjà → admin déjà initialisé, rien à faire
	existing := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: tokenSecretName, Namespace: instance.Namespace}, existing); err == nil {
		instance.Status.AdminTokenSecretRef = tokenSecretName
		return nil
	}

	// Lire le password cible depuis le Secret utilisateur
	adminSecret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      instance.Spec.AdminSecretRef,
		Namespace: instance.Namespace,
	}, adminSecret); err != nil {
		return fmt.Errorf("getting admin secret: %w", err)
	}
	newPassword := string(adminSecret.Data["password"])
	if newPassword == "" {
		return fmt.Errorf("admin secret %q missing key 'password'", instance.Spec.AdminSecretRef)
	}

	// Essayer d'abord avec le nouveau password (endpoint authentifié = ValidateAuth)
	clientNewPass := r.NewSonarClientWithPassword(serviceURL, "admin", newPassword)
	if err := clientNewPass.ValidateAuth(ctx); err != nil {
		// Nouveau password incorrect → le password n'a pas encore été changé → on le change
		clientDefault := r.NewSonarClientWithPassword(serviceURL, "admin", defaultAdminPassword)
		if err := clientDefault.ChangeAdminPassword(ctx, defaultAdminPassword, newPassword); err != nil {
			return fmt.Errorf("changing admin password: %w", err)
		}
		// Recréer le client avec le nouveau password maintenant valide
		clientNewPass = r.NewSonarClientWithPassword(serviceURL, "admin", newPassword)
	}

	// Nom unique incluant le namespace pour éviter les collisions entre instances homonymes
	tokenName := fmt.Sprintf("%s-%s-operator", instance.Namespace, instance.Name)
	token, err := clientNewPass.GenerateToken(ctx, tokenName, "USER_TOKEN", "")
	if err != nil {
		return fmt.Errorf("generating admin token: %w", err)
	}

	// Stocker le token dans un Secret owned par l'instance
	tokenSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tokenSecretName,
			Namespace: instance.Namespace,
		},
		Data: map[string][]byte{
			"token": []byte(token.Token),
		},
	}
	if err := controllerutil.SetControllerReference(instance, tokenSecret, r.Scheme); err != nil {
		return err
	}
	if err := r.Create(ctx, tokenSecret); err != nil {
		return fmt.Errorf("creating admin token secret: %w", err)
	}

	instance.Status.AdminTokenSecretRef = tokenSecretName
	r.Recorder.Event(instance, corev1.EventTypeNormal, "AdminInitialized",
		fmt.Sprintf("Admin token stored in Secret %q", tokenSecretName))
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

	// Mettre à jour uniquement si le contenu change pour éviter un Update toutes les 30s
	desiredHash := podSpecHash(desired.Spec.Template.Spec)
	existingHash := podSpecHash(existing.Spec.Template.Spec)
	if desiredHash == existingHash {
		return nil
	}

	existing.Spec.Template.Spec.InitContainers = desired.Spec.Template.Spec.InitContainers
	existing.Spec.Template.Spec.Containers = desired.Spec.Template.Spec.Containers
	existing.Spec.Template.Spec.SecurityContext = desired.Spec.Template.Spec.SecurityContext
	return r.Update(ctx, existing)
}

func (r *SonarQubeInstanceReconciler) reconcileHeadlessService(ctx context.Context, instance *sonarqubev1alpha1.SonarQubeInstance) error {
	desired := buildHeadlessService(instance)

	if err := controllerutil.SetControllerReference(instance, desired, r.Scheme); err != nil {
		return err
	}

	existing := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if errors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	return err
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
	if err != nil {
		return err
	}
	// ClusterIP est immuable — on ne met à jour que les Ports et le Type
	existing.Spec.Ports = desired.Spec.Ports
	existing.Spec.Type = desired.Spec.Type
	return r.Update(ctx, existing)
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
	if err != nil {
		return err
	}
	existing.Spec.Rules = desired.Spec.Rules
	existing.Spec.IngressClassName = desired.Spec.IngressClassName
	return r.Update(ctx, existing)
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
	extensionsSize := instance.Spec.Persistence.ExtensionsSize
	if extensionsSize == "" {
		extensionsSize = "1Gi"
	}

	var storageClassName *string
	if instance.Spec.Persistence.StorageClass != "" {
		sc := instance.Spec.Persistence.StorageClass
		storageClassName = &sc
	}

	replicas := int32(1)

	privileged := true
	fsGroup := int64(1000)

	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: instance.Name + "-headless",
			Selector:    &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					// fsGroup 1000 correspond à l'UID de l'image sonarqube officielle.
					// Sans ça, le PVC monté est root:root et SonarQube crashe au démarrage.
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup: &fsGroup,
					},
					// Elasticsearch embarqué dans SonarQube exige vm.max_map_count >= 524288.
					// Ce init container règle le paramètre noyau avant que le pod démarre.
					InitContainers: []corev1.Container{
						{
							Name:    "sysctl",
							Image:   "busybox:1.36",
							Command: []string{"sysctl", "-w", "vm.max_map_count=524288"},
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privileged,
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:      "sonarqube",
							Image:     image,
							Resources: resources,
							Ports: []corev1.ContainerPort{
								{Name: "http", ContainerPort: 9000, Protocol: corev1.ProtocolTCP},
							},
							Env: r.buildEnvVars(instance),
							// startupProbe laisse jusqu'à 10 min au démarrage initial (Elasticsearch + migrations DB).
							// Une fois SonarQube UP, readiness et liveness prennent le relais rapidement.
							StartupProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/api/system/status",
										Port: intstr.FromInt32(9000),
									},
								},
								FailureThreshold: 60,
								PeriodSeconds:    10,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/api/system/status",
										Port: intstr.FromInt32(9000),
									},
								},
								PeriodSeconds:    10,
								FailureThreshold: 3,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/api/system/status",
										Port: intstr.FromInt32(9000),
									},
								},
								PeriodSeconds:    30,
								FailureThreshold: 5,
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "data", MountPath: "/opt/sonarqube/data"},
								{Name: "extensions", MountPath: "/opt/sonarqube/extensions"},
							},
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "data"},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						StorageClassName: storageClassName,
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse(storageSize),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "extensions"},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						StorageClassName: storageClassName,
						Resources: corev1.VolumeResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse(extensionsSize),
							},
						},
					},
				},
			},
		},
	}
}

func (r *SonarQubeInstanceReconciler) buildEnvVars(instance *sonarqubev1alpha1.SonarQubeInstance) []corev1.EnvVar {
	envVars := []corev1.EnvVar{
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
	}
	if instance.Spec.JvmOptions != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "SONAR_WEB_JAVAADDITIONALOPTS",
			Value: instance.Spec.JvmOptions,
		})
	}
	return envVars
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

	var ingressClassName *string
	if instance.Spec.Ingress.IngressClassName != "" {
		ingressClassName = &instance.Spec.Ingress.IngressClassName
	}

	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: ingressClassName,
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
		Owns(&corev1.Secret{}).
		Named("sonarqubeinstance").
		Complete(r)
}
