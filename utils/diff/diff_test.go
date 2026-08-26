package diff

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestCreateTwoWayMergeInvalidOrig(t *testing.T) {
	_, _, err := CreateTwoWayMergePatch(make(chan int), nil, nil)
	assert.NotNil(t, err)
}

func TestCreateTwoWayMergeInvalidNewObj(t *testing.T) {
	rollout := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}
	_, _, err := CreateTwoWayMergePatch(rollout, make(chan int), nil)
	assert.NotNil(t, err)
}

func TestCreateTwoWayMergeInvalidDataStruct(t *testing.T) {
	rollout := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}
	rollout2 := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: v1alpha1.RolloutSpec{
			Replicas: ptr.To[int32](1),
		},
	}
	_, _, err := CreateTwoWayMergePatch(rollout, rollout2, nil)
	assert.Equal(t, err, fmt.Errorf("expected a struct, but received a nil"))
}

func TestCreateTwoWayMergeCreatePatch(t *testing.T) {
	rollout := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}
	rollout2 := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: v1alpha1.RolloutSpec{
			Replicas: ptr.To[int32](1),
		},
	}
	patch, isPatched, err := CreateTwoWayMergePatch(rollout, rollout2, v1alpha1.Rollout{})
	assert.Nil(t, err)
	assert.True(t, isPatched)
	assert.Equal(t, `{"spec":{"replicas":1}}`, string(patch))
}

func TestCreateTwoWayMergeNoPatch(t *testing.T) {
	rollout := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}
	rollout2 := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}
	patch, isPatched, err := CreateTwoWayMergePatch(rollout, rollout2, v1alpha1.Rollout{})
	assert.Nil(t, err)
	assert.False(t, isPatched)
	assert.Equal(t, "{}", string(patch))
}

func TestCreateTwoWayMergePatchWithResourceVersion(t *testing.T) {
	orig := v1alpha1.Rollout{Status: v1alpha1.RolloutStatus{Replicas: 1}}
	updated := v1alpha1.Rollout{Status: v1alpha1.RolloutStatus{Replicas: 2}}

	patch, isPatched, err := CreateTwoWayMergePatchWithResourceVersion(orig, updated, v1alpha1.Rollout{}, "12345")
	assert.Nil(t, err)
	assert.True(t, isPatched)
	assert.Contains(t, string(patch), `"resourceVersion":"12345"`)
	assert.Contains(t, string(patch), `"status":{"replicas":2}`)
}

func TestCreateTwoWayMergePatchWithResourceVersionNoChange(t *testing.T) {
	orig := v1alpha1.Rollout{Status: v1alpha1.RolloutStatus{Replicas: 1}}
	updated := v1alpha1.Rollout{Status: v1alpha1.RolloutStatus{Replicas: 1}}

	// No status change: must not emit a resourceVersion-only patch.
	patch, isPatched, err := CreateTwoWayMergePatchWithResourceVersion(orig, updated, v1alpha1.Rollout{}, "12345")
	assert.Nil(t, err)
	assert.False(t, isPatched)
	assert.Equal(t, "{}", string(patch))
}

func TestCreateTwoWayMergePatchWithResourceVersionEmpty(t *testing.T) {
	orig := v1alpha1.Rollout{Status: v1alpha1.RolloutStatus{Replicas: 1}}
	updated := v1alpha1.Rollout{Status: v1alpha1.RolloutStatus{Replicas: 2}}

	// Empty resourceVersion: fail open, behave like CreateTwoWayMergePatch.
	patch, isPatched, err := CreateTwoWayMergePatchWithResourceVersion(orig, updated, v1alpha1.Rollout{}, "")
	assert.Nil(t, err)
	assert.True(t, isPatched)
	assert.NotContains(t, string(patch), "resourceVersion")
}

func TestInjectResourceVersion(t *testing.T) {
	// merges into existing metadata
	out, err := InjectResourceVersion([]byte(`{"metadata":{"annotations":{"a":"b"}},"spec":{"x":1}}`), "99")
	assert.Nil(t, err)
	assert.Contains(t, string(out), `"resourceVersion":"99"`)
	assert.Contains(t, string(out), `"a":"b"`)

	// adds metadata when missing
	out, err = InjectResourceVersion([]byte(`{"spec":{"x":1}}`), "99")
	assert.Nil(t, err)
	assert.Contains(t, string(out), `"resourceVersion":"99"`)

	// no-op on empty resourceVersion
	out, err = InjectResourceVersion([]byte(`{"spec":{"x":1}}`), "")
	assert.Nil(t, err)
	assert.Equal(t, `{"spec":{"x":1}}`, string(out))
}
