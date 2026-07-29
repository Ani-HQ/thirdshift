//go:build windows

package identity

func CredentialBackendDescription() string {
	return "windows-credential-manager-stub"
}
