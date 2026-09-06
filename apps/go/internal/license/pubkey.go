package license

// PublicKeyBase64 is the Ed25519 public key that verifies licence tokens,
// standard base64 of the 32 raw bytes.
//
// The matching private key lives only in the private licence-signing tool
// (documented in docs/licensing.md as `cmd/licensegen`, kept in a separate
// private repository and never shipped in an image). Publishing this key is
// intentional and safe: it verifies signatures, it cannot create them.
//
// It can be overridden at build time without editing this file:
//
//	go build -ldflags "-X pipelogiq/internal/license.PublicKeyBase64=<base64>"
//
// The placeholder below is the all-zero key, which no real token can be signed
// against. A build that keeps it is Community-only, which is the correct
// default for the open repository.
var PublicKeyBase64 = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
