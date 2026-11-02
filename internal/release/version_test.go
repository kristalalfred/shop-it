package release

import (
	"encoding/json"
	"os"
	"testing"
)

func TestBundledManifestsCarryTheReleaseVersion(t *testing.T) {
	for _, path := range []string{"../../plugin/assets/plugin.json", "../../extension/assets/manifest.json", "../../extension/assets/manifest.firefox.json"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(raw, &manifest); err != nil || manifest.Version != Version {
			t.Fatalf("%s has version %q, want %s", path, manifest.Version, Version)
		}
	}
}
