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
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestValidateInstanceRefNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("registering scheme: %v", err)
	}

	makeInstance := func(ns, name, annotation string) *SonarQubeInstance {
		obj := &SonarQubeInstance{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		}
		if annotation != "" {
			obj.Annotations = map[string]string{CrossNamespaceOptInAnnotation: annotation}
		}
		return obj
	}

	cases := []struct {
		name          string
		callerNS      string
		ref           InstanceRef
		instances     []runtime.Object
		wantErr       bool
		wantErrSubstr string
	}{
		{
			name:     "empty namespace defaults to caller — allowed",
			callerNS: "team-a",
			ref:      InstanceRef{Name: "sq", Namespace: ""},
			wantErr:  false,
		},
		{
			name:     "same-namespace ref — allowed",
			callerNS: "team-a",
			ref:      InstanceRef{Name: "sq", Namespace: "team-a"},
			wantErr:  false,
		},
		{
			name:          "cross-ns, target instance missing — rejected",
			callerNS:      "team-a",
			ref:           InstanceRef{Name: "sq", Namespace: "shared"},
			instances:     nil,
			wantErr:       true,
			wantErrSubstr: "cannot verify target SonarQubeInstance",
		},
		{
			name:          "cross-ns, no annotation — rejected",
			callerNS:      "team-a",
			ref:           InstanceRef{Name: "sq", Namespace: "shared"},
			instances:     []runtime.Object{makeInstance("shared", "sq", "")},
			wantErr:       true,
			wantErrSubstr: "does not carry the",
		},
		{
			name:      "cross-ns, annotation lists caller — allowed",
			callerNS:  "team-a",
			ref:       InstanceRef{Name: "sq", Namespace: "shared"},
			instances: []runtime.Object{makeInstance("shared", "sq", "team-a,team-b")},
			wantErr:   false,
		},
		{
			name:      "cross-ns, annotation has whitespace around entries — allowed",
			callerNS:  "team-a",
			ref:       InstanceRef{Name: "sq", Namespace: "shared"},
			instances: []runtime.Object{makeInstance("shared", "sq", "  team-a , team-b  ")},
			wantErr:   false,
		},
		{
			name:          "cross-ns, annotation excludes caller — rejected",
			callerNS:      "team-c",
			ref:           InstanceRef{Name: "sq", Namespace: "shared"},
			instances:     []runtime.Object{makeInstance("shared", "sq", "team-a,team-b")},
			wantErr:       true,
			wantErrSubstr: "is not in the allowlist",
		},
		{
			name:      "cross-ns, wildcard annotation — allowed",
			callerNS:  "team-z",
			ref:       InstanceRef{Name: "sq", Namespace: "shared"},
			instances: []runtime.Object{makeInstance("shared", "sq", "*")},
			wantErr:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tc.instances...).Build()
			err := ValidateInstanceRefNamespace(context.Background(), c, tc.callerNS, tc.ref)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if tc.wantErrSubstr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErrSubstr)) {
				t.Fatalf("expected error to contain %q, got %v", tc.wantErrSubstr, err)
			}
		})
	}
}
