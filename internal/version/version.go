package version

// Version is the build shown in the console. A packaged binary replaces it
// with -ldflags "-X dmsx/internal/version.Version=…".
var Version = "dev"
