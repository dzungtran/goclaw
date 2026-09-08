package providers

// appVersion mirrors the build-time version (set via -ldflags on cmd.Version).
// Used for client-identity headers such as the OpenCode User-Agent.
var appVersion = "dev"

// SetAppVersion wires the binary's build version into the providers package.
// Called from cmd init; safe to skip (headers fall back to "goclaw/dev").
func SetAppVersion(v string) {
	if v != "" {
		appVersion = v
	}
}
