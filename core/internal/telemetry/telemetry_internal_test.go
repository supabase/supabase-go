package telemetry

import "testing"

func TestHeaderVersion(t *testing.T) {
	testCases := []struct {
		name     string
		recorded string
		want     string
	}{
		{name: "release", recorded: "v0.1.0", want: "0.1.0"},
		{name: "pre-release", recorded: "v0.1.0-alpha.4", want: "0.1.0-alpha.4"},
		{name: "pseudo-version", recorded: "v0.0.0-20260101120000-0123456789ab", want: "0.0.0-20260101120000-0123456789ab"},
		{name: "workspace module", recorded: "(devel)", want: unknownVersion},
		{name: "no version", recorded: "", want: unknownVersion},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := headerVersion(testCase.recorded); got != testCase.want {
				t.Errorf("headerVersion(%q) = %q, want %q", testCase.recorded, got, testCase.want)
			}
		})
	}
}

func TestPlatformName(t *testing.T) {
	testCases := []struct {
		goos string
		want string
	}{
		{goos: "aix", want: "AIX"},
		{goos: "android", want: "Android"},
		{goos: "darwin", want: "macOS"},
		{goos: "dragonfly", want: "DragonFly"},
		{goos: "freebsd", want: "FreeBSD"},
		{goos: "ios", want: "iOS"},
		{goos: "illumos", want: "Illumos"},
		{goos: "js", want: "JavaScript"},
		{goos: "linux", want: "Linux"},
		{goos: "netbsd", want: "NetBSD"},
		{goos: "openbsd", want: "OpenBSD"},
		{goos: "solaris", want: "Solaris"},
		{goos: "windows", want: "Windows"},
		{goos: "wasip1", want: "WASIp1"},
		// A GOOS without a mapping is sent verbatim.
		{goos: "plan9", want: "plan9"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.goos, func(t *testing.T) {
			if got := platformName(testCase.goos); got != testCase.want {
				t.Errorf("platformName(%q) = %q, want %q", testCase.goos, got, testCase.want)
			}
		})
	}
}
