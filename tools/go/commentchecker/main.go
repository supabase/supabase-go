// Command commentchecker validates the external URLs in the comments of the
// repository's published Go source files. Every URL must use HTTPS, sit on an
// allowed domain, resolve to an HTTP 200 HTML document within two permanent
// (301 or 308) redirect hops and, when the URL carries a fragment, name an id
// present in that document.
//
// The module directories given as arguments are walked for .go files,
// skipping test files, internal and testdata directories and nested modules.
// A URL listed as ignored in the configuration is skipped without any
// checks, and an ignored URL that matches no URL in the comments is itself a
// failure. Each remaining URL is fetched at most once per run, keyed by the
// URL stripped of its query and fragment, with each fetch announced on
// standard output. A fetch answered with HTTP 429 from a domain the
// configuration lists as rate limited is tolerated: the rate limiting is
// announced, surfaced as a warning annotation when running in GitHub Actions
// and the URL passes unverified. A 429 from any other domain is a failure.
// Every failing URL occurrence is reported to standard error with its file
// path and line number, and any failure exits non-zero.
//
// The repository entry point is ./scripts/comment-check.sh, which passes the
// go.work workspace modules and the committed configuration:
//
//	commentchecker -configuration comment-checker.yaml <module-directory> ...
package main

// cSpell:ignore GITHUB

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// configuration mirrors the YAML configuration file: the fully-qualified
// domain names the checker may probe with HTTP GET requests, the domains
// whose rate limiting is tolerated and the URLs excused from checking.
type configuration struct {
	AllowedDomains     []string `yaml:"allowed-domains"`
	RateLimitedDomains []string `yaml:"rate-limited-domains"`
	IgnoredURLs        []string `yaml:"ignored-urls"`
}

// policy is the parsed configuration: the domains the prober may GET, the
// domains whose HTTP 429 responses are tolerated and the ignored URLs
// (matched exactly as written in a comment).
type policy struct {
	allowedDomains     map[string]struct{}
	rateLimitedDomains map[string]struct{}
	ignoredURLs        map[string]struct{}
}

func main() {
	configurationPath := flag.String("configuration", "", "path to the YAML configuration file listing allowed domains and ignored URLs")
	flag.Parse()
	if *configurationPath == "" || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: commentchecker -configuration <file> <module-directory> ...")
		os.Exit(2)
	}
	if err := run(*configurationPath, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run returns the first configuration or file-system error, or an error
// summarizing the problems found - failing URL occurrences or stale ignored
// URLs - after every check has run and every problem printed.
func run(configurationPath string, moduleDirectories []string) error {
	checkPolicy, err := loadPolicy(configurationPath)
	if err != nil {
		return err
	}

	var occurrences []urlOccurrence
	fileCount := 0
	for _, moduleDirectory := range moduleDirectories {
		sourceFiles, err := publicSourceFiles(moduleDirectory)
		if err != nil {
			return err
		}
		fileCount += len(sourceFiles)
		for _, sourceFile := range sourceFiles {
			fileOccurrences, err := extractURLOccurrences(sourceFile)
			if err != nil {
				return err
			}
			occurrences = append(occurrences, fileOccurrences...)
		}
	}

	prober := newProber(checkPolicy)
	type failedOccurrence struct {
		occurrence urlOccurrence
		reason     string
	}
	var failures []failedOccurrence
	var rateLimitedOccurrences []urlOccurrence
	ignoredCount := 0
	matchedIgnoredURLs := make(map[string]struct{})
	for _, occurrence := range occurrences {
		if _, ignored := checkPolicy.ignoredURLs[occurrence.url]; ignored {
			ignoredCount++
			matchedIgnoredURLs[occurrence.url] = struct{}{}
			continue
		}
		reason, rateLimited := prober.check(occurrence.url)
		if reason != "" {
			failures = append(failures, failedOccurrence{occurrence, reason})
			continue
		}
		if rateLimited {
			rateLimitedOccurrences = append(rateLimitedOccurrences, occurrence)
		}
	}

	slices.SortFunc(failures, func(left, right failedOccurrence) int {
		return compareOccurrences(left.occurrence, right.occurrence)
	})
	for _, failure := range failures {
		fmt.Fprintf(os.Stderr, "%s:%d: %s: %s\n", failure.occurrence.file, failure.occurrence.line, failure.occurrence.url, failure.reason)
	}

	slices.SortFunc(rateLimitedOccurrences, compareOccurrences)
	emitGitHubWarnings(rateLimitedOccurrences)

	var staleIgnoredURLs []string
	for ignoredURL := range checkPolicy.ignoredURLs {
		if _, matched := matchedIgnoredURLs[ignoredURL]; !matched {
			staleIgnoredURLs = append(staleIgnoredURLs, ignoredURL)
		}
	}
	slices.Sort(staleIgnoredURLs)
	for _, staleIgnoredURL := range staleIgnoredURLs {
		fmt.Fprintf(os.Stderr, "%s: ignored URL matches no URL in the checked comments: %s\n", configurationPath, staleIgnoredURL)
	}

	checkedCount := len(occurrences) - ignoredCount
	fmt.Printf("checked %d URL occurrences (%d URLs fetched, %d rate limited, %d ignored) across %d files\n", checkedCount, prober.fetchCount(), prober.rateLimitedCount(), ignoredCount, fileCount)
	if len(failures) > 0 {
		return fmt.Errorf("%d of %d URL occurrences failed validation", len(failures), checkedCount)
	}
	if len(staleIgnoredURLs) > 0 {
		return fmt.Errorf("stale ignored-urls entries in %s: %d", configurationPath, len(staleIgnoredURLs))
	}
	return nil
}

// loadPolicy reads the configuration file, lowercasing domains and keeping
// ignored URLs exact. An unknown configuration key, an empty allowed-domains
// list and a rate-limited domain missing from allowed-domains are all errors,
// while the rate-limited and ignored lists may be absent or empty.
func loadPolicy(configurationPath string) (policy, error) {
	configurationBytes, err := os.ReadFile(configurationPath)
	if err != nil {
		return policy{}, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(configurationBytes))
	decoder.KnownFields(true)
	var parsed configuration
	if err := decoder.Decode(&parsed); err != nil && !errors.Is(err, io.EOF) {
		return policy{}, fmt.Errorf("parsing %s: %w", configurationPath, err)
	}
	if len(parsed.AllowedDomains) == 0 {
		return policy{}, fmt.Errorf("%s lists no allowed domains", configurationPath)
	}
	loaded := policy{
		allowedDomains:     make(map[string]struct{}, len(parsed.AllowedDomains)),
		rateLimitedDomains: make(map[string]struct{}, len(parsed.RateLimitedDomains)),
		ignoredURLs:        make(map[string]struct{}, len(parsed.IgnoredURLs)),
	}
	for _, domain := range parsed.AllowedDomains {
		loaded.allowedDomains[strings.ToLower(domain)] = struct{}{}
	}
	for _, domain := range parsed.RateLimitedDomains {
		lowercased := strings.ToLower(domain)
		if _, allowed := loaded.allowedDomains[lowercased]; !allowed {
			return policy{}, fmt.Errorf("%s: rate-limited domain %q is not in allowed-domains", configurationPath, domain)
		}
		loaded.rateLimitedDomains[lowercased] = struct{}{}
	}
	for _, ignoredURL := range parsed.IgnoredURLs {
		loaded.ignoredURLs[ignoredURL] = struct{}{}
	}
	return loaded, nil
}

// GitHub Actions workflow commands require percent, carriage return and line
// feed escaped everywhere, with colons and commas additionally escaped inside
// property values.
var (
	annotationMessageEscaper  = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	annotationPropertyEscaper = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C")
)

// emitGitHubWarnings prints a GitHub Actions warning workflow command for
// each occurrence, attaching an annotation to the occurrence's file and line
// in the run's web UI. Outside GitHub Actions (the GITHUB_ACTIONS environment
// variable is not "true") it prints nothing.
func emitGitHubWarnings(occurrences []urlOccurrence) {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		return
	}
	for _, occurrence := range occurrences {
		fmt.Printf("::warning file=%s,line=%d,title=URL not verified (rate limited)::%s\n",
			annotationPropertyEscaper.Replace(occurrence.file),
			occurrence.line,
			annotationMessageEscaper.Replace(occurrence.url+" answered HTTP status 429 (rate limited), so the link could not be verified"))
	}
}
