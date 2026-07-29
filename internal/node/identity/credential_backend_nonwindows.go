//go:build !windows

package identity

func CredentialBackendDescription() string {
	return "restricted-local-file"
}
