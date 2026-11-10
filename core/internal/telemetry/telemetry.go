// Package telemetry provides utilities to provide informative and semantically correct
// data to Supabase services that helps identify this SDK.
package telemetry

import (
	"fmt"
	"path"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/supabase/supabase-go/core"
)

const rootClientName = "supabase-go"

// unknownVersion is the version a client module reports when build
// information records none for it: a binary built within the SDK's own
// module tree or a workspace that includes it, or one carrying no module
// information at all. A version always begins with a digit.
const unknownVersion = "0.0.0"

// ClientInformationHeaderValue returns a value suitable for use for the
// X-Client-Info header sent with HTTP requests submitted to Supabase services.
//
// The returned value is delimited with "; " (semi-colon followed by a space),
// always in the following order:
//   - entry module and its version (e.g. "supabase-go/0.1.0" or
//     "postgrest-go/0.0.0" when build information records no version)
//   - runtime (always "go")
//   - runtime-version (e.g. "1.26.5" for go1.26.5)
//   - platform (e.g. "Linux" or "macOS")
//   - platform-architecture (e.g. "amd64" or "arm64")
//
// entryModulePath identifies the SDK client the value describes. It panics
// when that module is not expected to emit telemetry, or when the running
// binary's module information places the build outside the SDK's own module
// tree yet holds no dependency record for that module.
func ClientInformationHeaderValue(entryModulePath core.ModulePath) string {
	headerValue, ok := clientInformationHeaderValues()[entryModulePath]
	if !ok {
		panic(fmt.Sprintf("telemetry: no client name registered for module %q", entryModulePath))
	}
	return headerValue
}

var clientInformationHeaderValues = sync.OnceValue(buildClientInformationHeaderValues)

func buildClientInformationHeaderValues() map[core.ModulePath]string {
	clientNames := map[core.ModulePath]string{
		core.ModulePathRoot:      rootClientName,
		core.ModulePathPostgrest: "postgrest-go",
		core.ModulePathAuth:      "auth-go",
	}

	clientBases := make(map[core.ModulePath]string, len(clientNames))

	buildInformation, ok := debug.ReadBuildInfo()
	if ok && buildInformation.Main.Path != "" {
		if builtWithinSDK(buildInformation) {
			for modulePath, clientName := range clientNames {
				clientBases[modulePath] = clientName + "/" + unknownVersion
			}
		} else {
			// The binary was built outside the SDK's module tree, so client module
			// versions are resolvable only from its recorded dependencies.
			// We populate a value only for client modules we can find there.
			for _, module := range buildInformation.Deps {
				modulePath := core.ModulePath(module.Path)
				if clientName, found := clientNames[modulePath]; found {
					clientBases[modulePath] = clientName + "/" + headerVersion(module.Version)
				}
			}
		}
	} else {
		// The binary carries no module information: either build information is
		// absent entirely, or it lacks module records, as with binaries built
		// without module support. No client module's version is knowable, so
		// synthesize a value for every possible client module.
		for modulePath, clientName := range clientNames {
			clientBases[modulePath] = clientName + "/" + unknownVersion
		}
	}

	common := "; runtime=go"

	if goRuntimeVersion, didFindGoPrefix := strings.CutPrefix(runtime.Version(), "go"); didFindGoPrefix {
		common += "; runtime-version=" + goRuntimeVersion
	}

	common += "; platform=" + platformName(runtime.GOOS)

	// Our platform-architecture values are emitted in their original form.
	// This is because, at the time of implementation, we appear to be the first Supabase SDK
	// to emit this information.
	common += "; platform-architecture=" + runtime.GOARCH

	headerValues := make(map[core.ModulePath]string, len(clientBases))
	for modulePath, clientBase := range clientBases {
		headerValues[modulePath] = clientBase + common
	}

	return headerValues
}

// headerVersion returns the version a build-information dependency record
// gives a client module, without its "v" prefix. A record that carries no
// version, which Go writes as "(devel)" for a module built from a workspace,
// gives unknownVersion.
func headerVersion(recorded string) string {
	if recorded == "" || recorded == "(devel)" {
		return unknownVersion
	}
	return strings.TrimPrefix(recorded, "v")
}

// platformName returns the platform value for a GOOS.
//
// Our platform values align with what other Supabase SDKs emit, where defined.
// For OS values not emitted by other Supabase SDKs we're following the same pattern
// of using the human-formatted form (e.g. capitalized "BSD" and 'F' in "FreeBSD").
func platformName(goos string) string {
	switch goos {
	case "aix":
		return "AIX"
	case "android":
		return "Android"
	case "darwin":
		return "macOS"
	case "dragonfly":
		return "DragonFly"
	case "freebsd":
		return "FreeBSD"
	case "ios":
		return "iOS"
	case "illumos":
		return "Illumos"
	case "js":
		return "JavaScript"
	case "linux":
		return "Linux"
	case "netbsd":
		return "NetBSD"
	case "openbsd":
		return "OpenBSD"
	case "solaris":
		return "Solaris"
	case "windows":
		return "Windows"
	case "wasip1":
		return "WASIp1"

	// For OS for which we don't have a mapping we emit the runtime.GOOS value verbatim,
	// including for "plan9" (final release was made in January 2015!).
	default:
		return goos
	}
}

// builtWithinSDK reports whether the running binary is built or tested from
// within this SDK's own module tree rather than consumed as a dependency. In
// that case a client module is the main module or a workspace sibling, so its
// version is not resolvable from build information.
func builtWithinSDK(buildInformation *debug.BuildInfo) bool {
	// All SDK modules share the repository's base import path, established
	// here as the parent of this module(core)'s path.
	sdkModuleTreeBase := path.Dir(string(core.ModulePathPostgrest))
	return strings.HasPrefix(buildInformation.Main.Path, sdkModuleTreeBase+"/")
}

// Regarding runtime.GOOS and runtime.GOARCH...
// For reference, here is a snapshot of `go tool dist list` on 21 July 2026, run from go version go1.26.5 darwin/arm64:
// aix/ppc64
// android/386
// android/amd64
// android/arm
// android/arm64
// darwin/amd64
// darwin/arm64
// dragonfly/amd64
// freebsd/386
// freebsd/amd64
// freebsd/arm
// freebsd/arm64
// illumos/amd64
// ios/amd64
// ios/arm64
// js/wasm
// linux/386
// linux/amd64
// linux/arm
// linux/arm64
// linux/loong64
// linux/mips
// linux/mips64
// linux/mips64le
// linux/mipsle
// linux/ppc64
// linux/ppc64le
// linux/riscv64
// linux/s390x
// netbsd/386
// netbsd/amd64
// netbsd/arm
// netbsd/arm64
// openbsd/386
// openbsd/amd64
// openbsd/arm
// openbsd/arm64
// openbsd/ppc64
// openbsd/riscv64
// plan9/386
// plan9/amd64
// plan9/arm
// solaris/amd64
// wasip1/wasm
// windows/386
// windows/amd64
// windows/arm64
