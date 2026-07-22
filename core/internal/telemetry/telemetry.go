// Package telemetry provides utilities to provide informative and semantically correct
// data to Supabase services that helps identify this SDK.
package telemetry

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/supabase/supabase-go/core"
)

const rootClientName = "supabase-go"

// ClientInformationHeaderValue returns a value suitable for use for the
// X-Client-Info header sent with HTTP requests submitted to Supabase services.
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
	}

	clientBases := make(map[core.ModulePath]string, len(clientNames))

	buildInformation, ok := debug.ReadBuildInfo()
	if ok && buildInformation.Main.Path != "" {
		if builtWithinSDK(buildInformation) {
			for modulePath, clientName := range clientNames {
				clientBases[modulePath] = clientName + "/(devel)"
			}
		} else {
			// The binary was built outside the SDK's module tree, so client module
			// versions are resolvable only from its recorded dependencies.
			// We populate a value only for client modules we can find there.
			for _, module := range buildInformation.Deps {
				modulePath := core.ModulePath(module.Path)
				if clientName, found := clientNames[modulePath]; found {
					clientBases[modulePath] = clientName + "/" + strings.TrimPrefix(module.Version, "v")
				}
			}
		}
	} else {
		// The binary carries no module information: either build information is
		// absent entirely, or it lacks module records - as with binaries built
		// without module support and test binaries from Go toolchains before 1.24.
		// No client module's version is knowable, so synthesize a value for every
		// possible client module.
		for modulePath, clientName := range clientNames {
			clientBases[modulePath] = clientName + "/0.0.0"
		}
	}

	common := "; runtime=go"

	if goRuntimeVersion, didFindGoPrefix := strings.CutPrefix(runtime.Version(), "go"); didFindGoPrefix {
		common += "; runtime-version=" + goRuntimeVersion
	}

	headerValues := make(map[core.ModulePath]string, len(clientBases))
	for modulePath, clientBase := range clientBases {
		headerValues[modulePath] = clientBase + common
	}

	return headerValues
}

// builtWithinSDK reports whether the running binary is built or tested from
// within this SDK's own module tree rather than consumed as a dependency. In
// that case a client module is the main module or a workspace sibling, so its
// version is not resolvable from build information.
func builtWithinSDK(buildInformation *debug.BuildInfo) bool {
	rootModulePath := string(core.ModulePathRoot)
	return buildInformation.Main.Path == rootModulePath ||
		strings.HasPrefix(buildInformation.Main.Path, rootModulePath+"/")
}
