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
// entryModulePath identifies the SDK client the value describes. It panics if
// that module is either not expected to emit telemetry or if that module's
// dependency isn't found in build information when the running binary was known
// to have been built with module support and isn't built or running tests within
// the SDK's own module tree.
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
	if ok {
		if builtWithinSDK(buildInformation) {
			for modulePath, clientName := range clientNames {
				clientBases[modulePath] = clientName + "/(devel)"
			}
		} else {
			// There was build information embedded in the running binary, which means
			// that it was built with module support.
			// We populate a value only for client modules we can find in dependencies.
			for _, module := range buildInformation.Deps {
				modulePath := core.ModulePath(module.Path)
				if clientName, found := clientNames[modulePath]; found {
					clientBases[modulePath] = clientName + "/" + strings.TrimPrefix(module.Version, "v")
				}
			}
		}
	} else {
		// There was no build information embedded in the running binary.
		// This means that it was not built with module support.
		// Our fallback is to synthesize a value for every possible client module.
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
