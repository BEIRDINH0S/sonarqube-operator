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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func instanceWithVersion(v string) *SonarQubeInstance {
	i := &SonarQubeInstance{}
	i.Spec.Version = v
	return i
}

func TestValidateUpdate_AllowsUpgrade(t *testing.T) {
	v := &SonarQubeInstanceValidator{}
	_, err := v.ValidateUpdate(context.Background(),
		instanceWithVersion("9.9"),
		instanceWithVersion("10.3"),
	)
	require.NoError(t, err)
}

func TestValidateUpdate_AllowsSameVersion(t *testing.T) {
	v := &SonarQubeInstanceValidator{}
	_, err := v.ValidateUpdate(context.Background(),
		instanceWithVersion("10.3"),
		instanceWithVersion("10.3"),
	)
	require.NoError(t, err)
}

func TestValidateUpdate_RejectsMajorDowngrade(t *testing.T) {
	v := &SonarQubeInstanceValidator{}
	_, err := v.ValidateUpdate(context.Background(),
		instanceWithVersion("10.3"),
		instanceWithVersion("9.9"),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be downgraded")
}

func TestValidateUpdate_RejectsMinorDowngrade(t *testing.T) {
	v := &SonarQubeInstanceValidator{}
	_, err := v.ValidateUpdate(context.Background(),
		instanceWithVersion("10.5"),
		instanceWithVersion("10.3"),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be downgraded")
}

func TestValidateUpdate_AllowsFullVersionFormat(t *testing.T) {
	v := &SonarQubeInstanceValidator{}
	_, err := v.ValidateUpdate(context.Background(),
		instanceWithVersion("10.3.0.82913"),
		instanceWithVersion("10.4.0.91234"),
	)
	require.NoError(t, err)
}

func TestValidateUpdate_AllowsEmptyVersion(t *testing.T) {
	v := &SonarQubeInstanceValidator{}
	_, err := v.ValidateUpdate(context.Background(),
		instanceWithVersion(""),
		instanceWithVersion("10.3"),
	)
	require.NoError(t, err)
}

func TestValidateCreate_AlwaysAllows(t *testing.T) {
	v := &SonarQubeInstanceValidator{}
	_, err := v.ValidateCreate(context.Background(), instanceWithVersion("10.3"))
	require.NoError(t, err)
}

func TestParseMajorMinor(t *testing.T) {
	tests := []struct {
		input     string
		wantMajor int
		wantMinor int
		wantErr   bool
	}{
		{"10.3", 10, 3, false},
		{"9.9", 9, 9, false},
		{"10.3.0.82913", 10, 3, false},
		{"invalid", 0, 0, true},
		{"10", 0, 0, true},
	}
	for _, tt := range tests {
		major, minor, err := parseMajorMinor(tt.input)
		if tt.wantErr {
			assert.Error(t, err, "input: %s", tt.input)
		} else {
			require.NoError(t, err, "input: %s", tt.input)
			assert.Equal(t, tt.wantMajor, major)
			assert.Equal(t, tt.wantMinor, minor)
		}
	}
}
