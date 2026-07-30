package models

import (
	"embed"
)

// Some model licenses (e.g. the Llama 3.2 Community License) require a copy
// of the agreement to accompany any distribution of the model materials.
// Upstream license files often live in gated repositories that a host node
// cannot fetch anonymously, so the texts are vendored here and embedded in
// the binary; the node writes them next to the cached model file.
//
//go:embed licenses/*.txt
var licenseTexts embed.FS

var licenseFileByIdentifier = map[string]string{
	"llama3.2": "licenses/llama-3.2-community-license.txt",
}

// LicenseTextFor returns the vendored license text for a manifest license
// identifier, and whether one exists.
func LicenseTextFor(identifier string) (string, bool) {
	file, ok := licenseFileByIdentifier[identifier]
	if !ok {
		return "", false
	}
	data, err := licenseTexts.ReadFile(file)
	if err != nil {
		return "", false
	}
	return string(data), true
}
