package diff

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/util/strategicpatch"
)

// CreateTwoWayMergePatch is a helper to construct a two-way merge patch from objects (instead of bytes)
func CreateTwoWayMergePatch(orig, new, dataStruct any) ([]byte, bool, error) {
	origBytes, err := json.Marshal(orig)
	if err != nil {
		return nil, false, err
	}
	newBytes, err := json.Marshal(new)
	if err != nil {
		return nil, false, err
	}
	patch, err := strategicpatch.CreateTwoWayMergePatch(origBytes, newBytes, dataStruct)
	if err != nil {
		return nil, false, err
	}
	return patch, string(patch) != "{}", nil
}

// CreateTwoWayMergePatchWithResourceVersion behaves like CreateTwoWayMergePatch but injects
// metadata.resourceVersion (which should be the orig object's ResourceVersion) into the patch
// so the apiserver enforces optimistic concurrency and returns a 409 Conflict when the live
// object has changed since it was read. The resourceVersion is only injected when there is an
// actual change, so we never emit a resourceVersion-only patch. When resourceVersion is empty
// it falls back to the unguarded behavior of CreateTwoWayMergePatch (fail open).
func CreateTwoWayMergePatchWithResourceVersion(orig, new, dataStruct any, resourceVersion string) ([]byte, bool, error) {
	patch, modified, err := CreateTwoWayMergePatch(orig, new, dataStruct)
	if err != nil || !modified {
		return patch, modified, err
	}
	patch, err = InjectResourceVersion(patch, resourceVersion)
	if err != nil {
		return nil, false, err
	}
	return patch, true, nil
}

// InjectResourceVersion adds metadata.resourceVersion to an existing JSON merge patch so the
// apiserver enforces optimistic concurrency. It is a no-op when resourceVersion is empty (fail
// open). It works for merge and strategic-merge patch bodies (objects), not JSON (RFC 6902)
// patches, which are arrays.
func InjectResourceVersion(patch []byte, resourceVersion string) ([]byte, error) {
	if resourceVersion == "" {
		return patch, nil
	}
	var m map[string]any
	if err := json.Unmarshal(patch, &m); err != nil {
		return nil, err
	}
	meta, _ := m["metadata"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	meta["resourceVersion"] = resourceVersion
	m["metadata"] = meta
	return json.Marshal(m)
}
