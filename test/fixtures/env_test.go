package fixtures

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/validation"
)

func TestDNSLabel(t *testing.T) {
	assert.Equal(t, "canarysetcanaryscale", dnsLabel("TestCanarySetCanaryScale", 50))
	assert.Equal(t, "canary-with-pause", dnsLabel("TestCanary/With Pause", 50))
	assert.Equal(t, "a-b", dnsLabel("Test__a__b__", 50))
	assert.Equal(t, "test", dnsLabel("Test", 50))
	assert.Equal(t, "test", dnsLabel("Test///", 50))
	// Truncation must not leave a trailing dash.
	assert.Equal(t, "abc", dnsLabel("Testabc-def", 4))

	long := "Test" + strings.Repeat("LongName", 20)
	name := namespacePrefix + dnsLabel(long, namespaceNameBudget) + "-" + "abcde"
	assert.LessOrEqual(t, len(name), 63)
	assert.Empty(t, validation.IsDNS1123Label(name))
}

func TestLabelValue(t *testing.T) {
	assert.Equal(t, "TestCanarySetCanaryScale", labelValue("TestCanarySetCanaryScale"))
	assert.Equal(t, "TestCanary_With_Pause", labelValue("TestCanary/With Pause"))
	for _, name := range []string{"TestCanary/With Pause", "Test/", "Test" + strings.Repeat("LongName", 20), "_x_"} {
		assert.Empty(t, validation.IsValidLabelValue(labelValue(name)), name)
	}
}

func TestRunPatternNames(t *testing.T) {
	const name = "TestCanaryAbortSwitchesCanaryServiceOnce"
	assert.True(t, runPatternNames(name, name))
	assert.True(t, runPatternNames("^"+name+"$", name))
	assert.True(t, runPatternNames("TestOther|^"+name+"$", name))
	assert.True(t, runPatternNames("^("+name+")$", name))
	// Matching along with other tests is not asking for it by name.
	assert.False(t, runPatternNames("", name))
	assert.False(t, runPatternNames("TestCanary", name))
	assert.False(t, runPatternNames("Test", name))
	assert.False(t, runPatternNames(name+"Extra", name))
}
