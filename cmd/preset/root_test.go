package preset

import (
	"bytes"
	"strings"
	"testing"
)

func TestListIncludesAuthenticationType(t *testing.T) {
	var out bytes.Buffer
	listCmd.SetOut(&out)
	t.Cleanup(func() { listCmd.SetOut(nil) })

	if err := listCmd.RunE(listCmd, nil); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"calypr gen3 provider-helper:gen3-profile",
		"cgc cgc bearer",
		"synapse synapse bearer",
		"terra terra google-adc",
	} {
		if fields := strings.Fields(out.String()); !containsConsecutive(fields, strings.Fields(want)) {
			t.Errorf("preset list output does not include %q:\n%s", want, out.String())
		}
	}
}

func containsConsecutive(values, want []string) bool {
	for i := 0; i+len(want) <= len(values); i++ {
		if strings.Join(values[i:i+len(want)], " ") == strings.Join(want, " ") {
			return true
		}
	}
	return false
}
