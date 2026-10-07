package addref

import "testing"

func TestAcceptanceAddRefExposesRemoteTypeForTerraReferences(t *testing.T) {
	if Cmd.Flags().Lookup("remote-type") == nil {
		t.Fatalf("expected add-ref to expose --remote-type so Terra DRS references can select a Terra resolver explicitly")
	}
}
