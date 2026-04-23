package security

import "strings"

// ProtectedConfigFiles are config filenames that should never be mutated by
// generic tool calls when the payload references sensitive settings.
var ProtectedConfigFiles = []string{
	"roboticus.toml",
	"config-overrides.toml",
}

// ProtectedConfigFields are exact or prefix-like patterns whose mutation is
// considered security-sensitive across both policy and post-inference guards.
var ProtectedConfigFields = []string{
	"scope_mode",
	"api_key",
	"admin_token",
	"database.path",
	"keystore",
	"keystore.",
	"trusted_proxy",
	"private_key",
	"server.bind",
	"server.tls_cert",
	"server.tls_key",
	"server.auth_token",
	"wallet.passphrase",
	"wallet.keyfile",
}

// ProtectedConfigSuffixes capture wildcard-like settings such as db_secret or
// refresh_token without needing to enumerate every key.
var ProtectedConfigSuffixes = []string{
	"_secret",
	"_token",
}

func ReferencesProtectedConfigFile(text string) bool {
	lower := strings.ToLower(text)
	for _, name := range ProtectedConfigFiles {
		if strings.Contains(lower, name) {
			return true
		}
	}
	return false
}

func MatchProtectedConfigPattern(text string) (string, bool) {
	lower := strings.ToLower(text)
	for _, field := range ProtectedConfigFields {
		if strings.Contains(lower, field) {
			return field, true
		}
	}
	for _, suffix := range ProtectedConfigSuffixes {
		if strings.Contains(lower, suffix) {
			return "*" + suffix, true
		}
	}
	return "", false
}
