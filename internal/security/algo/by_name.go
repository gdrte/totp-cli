package algo

// ByName returns the Algorithm registered under the given name
// (sha1, sha256, sha512). Unknown or empty names fall back to Default.
//
//nolint:ireturn // the concrete type depends on the name.
func ByName(name string) Algorithm {
	switch name {
	case "sha256":
		return SHA256{}
	case "sha512":
		return SHA512{}
	default:
		return Default{}
	}
}

// IsValidName reports whether the given name is a supported algorithm name.
// An empty name is valid and means the default algorithm.
func IsValidName(name string) bool {
	switch name {
	case "", "sha1", "sha256", "sha512":
		return true
	default:
		return false
	}
}
