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

package v1alpha1

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// +kubebuilder:webhook:path=/validate-sonarqube-sonarqube-io-v1alpha1-sonarqubeinstance,mutating=false,failurePolicy=ignore,sideEffects=None,groups=sonarqube.sonarqube.io,resources=sonarqubeinstances,verbs=create;update,versions=v1alpha1,name=vsonarqubeinstance.kb.io,admissionReviewVersions=v1

// SonarQubeInstanceValidator validates SonarQubeInstance resources.
type SonarQubeInstanceValidator struct{}

// SetupSonarQubeInstanceWebhookWithManager registers the validating webhook with the manager.
func SetupSonarQubeInstanceWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &SonarQubeInstance{}).
		WithValidator(&SonarQubeInstanceValidator{}).
		Complete()
}

func (v *SonarQubeInstanceValidator) ValidateCreate(_ context.Context, _ *SonarQubeInstance) (admission.Warnings, error) {
	return nil, nil
}

// ValidateUpdate rejects spec.version downgrades (e.g. 10.3 → 9.9).
func (v *SonarQubeInstanceValidator) ValidateUpdate(_ context.Context, oldInstance, newInstance *SonarQubeInstance) (admission.Warnings, error) {
	if err := validateVersionNotDowngraded(oldInstance.Spec.Version, newInstance.Spec.Version); err != nil {
		return nil, err
	}
	return nil, nil
}

func (v *SonarQubeInstanceValidator) ValidateDelete(_ context.Context, _ *SonarQubeInstance) (admission.Warnings, error) {
	return nil, nil
}

// validateVersionNotDowngraded returns an error if newVersion is lower than oldVersion.
// Versions are compared by major.minor only ("10.3.0.82913" → major=10, minor=3).
// Unparseable versions are allowed through to avoid blocking on unconventional formats.
func validateVersionNotDowngraded(oldVersion, newVersion string) error {
	if oldVersion == "" || newVersion == "" || oldVersion == newVersion {
		return nil
	}
	oldMajor, oldMinor, err := parseMajorMinor(oldVersion)
	if err != nil {
		return nil
	}
	newMajor, newMinor, err := parseMajorMinor(newVersion)
	if err != nil {
		return nil
	}
	if newMajor < oldMajor || (newMajor == oldMajor && newMinor < oldMinor) {
		return fmt.Errorf("spec.version cannot be downgraded from %q to %q", oldVersion, newVersion)
	}
	return nil
}

// parseMajorMinor extracts the major and minor components from a version string.
// Supports formats like "10.3", "10.3.0", "10.3.0.82913".
func parseMajorMinor(version string) (major, minor int, err error) {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("invalid version %q: need at least major.minor", version)
	}
	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid major in version %q", version)
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid minor in version %q", version)
	}
	return major, minor, nil
}
