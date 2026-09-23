package main

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// maximumRedirectHops is how many permanent redirects a fetch may follow
// before it fails.
const maximumRedirectHops = 2

// probeUserAgent identifies this tool to the sites it probes.
const probeUserAgent = "supabase-go-commentchecker (+https://github.com/supabase/supabase-go)"

// probeOutcome is the shared result of fetching one URL (query and fragment
// stripped): either the failure that applies to every occurrence of that URL,
// or the set of id attribute values in the resolved HTML document against
// which each occurrence's fragment is checked.
type probeOutcome struct {
	failure     string
	documentIDs map[string]struct{}
}

// prober fetches URLs sequentially and remembers every outcome, so a URL is
// fetched at most once per run however many comments mention it.
type prober struct {
	client         *http.Client
	allowedDomains map[string]struct{}
	outcomes       map[string]probeOutcome
}

// newProber returns a prober that GETs only HTTPS URLs on the allowed
// domains, following at most maximumRedirectHops permanent redirects.
func newProber(allowedDomains map[string]struct{}) *prober {
	p := &prober{
		allowedDomains: allowedDomains,
		outcomes:       make(map[string]probeOutcome),
	}
	p.client = &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: p.checkRedirect,
	}
	return p
}

// check validates one URL, returning the empty string when every check passes
// and otherwise the reason it fails. Checks run in order - HTTPS, allowed
// domain, fetch reaching HTTP 200, HTML document, fragment id - and the first
// failing check ends the run for that URL. The fetch is shared across
// occurrences through the outcome cache; the fragment check runs per URL.
func (p *prober) check(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Sprintf("not a valid URL: %v", err)
	}
	if err := p.validateTarget(parsedURL); err != nil {
		return err.Error()
	}
	outcome := p.fetchOnce(parsedURL)
	if outcome.failure != "" {
		return outcome.failure
	}
	if parsedURL.Fragment != "" {
		if _, found := outcome.documentIDs[parsedURL.Fragment]; !found {
			return fmt.Sprintf("fragment %q does not match any id in the document", "#"+parsedURL.Fragment)
		}
	}
	return ""
}

// fetchCount reports how many distinct URLs have been fetched.
func (p *prober) fetchCount() int {
	return len(p.outcomes)
}

// validateTarget applies the checks that need no request: the URL must use
// HTTPS and its domain must be in the allowed set.
func (p *prober) validateTarget(target *url.URL) error {
	if target.Scheme != "https" {
		return fmt.Errorf("URL scheme is %q, not https", target.Scheme)
	}
	domain := strings.ToLower(target.Hostname())
	if _, allowed := p.allowedDomains[domain]; !allowed {
		return fmt.Errorf("domain %q is not in the allowed domains", domain)
	}
	return nil
}

// checkRedirect enforces the redirect policy on each hop the client is about
// to follow: at most maximumRedirectHops hops, each caused by a permanent
// redirect status (301 or 308) and each landing on a target that passes
// validateTarget.
func (p *prober) checkRedirect(request *http.Request, via []*http.Request) error {
	if len(via) > maximumRedirectHops {
		return fmt.Errorf("more than %d redirect hops", maximumRedirectHops)
	}
	redirectStatus := request.Response.StatusCode
	if redirectStatus != http.StatusMovedPermanently && redirectStatus != http.StatusPermanentRedirect {
		return fmt.Errorf("redirect status %d, only 301 and 308 are followed", redirectStatus)
	}
	return p.validateTarget(request.URL)
}

// fetchOnce returns the cached outcome for the URL with its query and
// fragment stripped, fetching only on first sight of that stripped URL and
// announcing each fetch on standard output as progress.
func (p *prober) fetchOnce(parsedURL *url.URL) probeOutcome {
	strippedURL := *parsedURL
	strippedURL.RawQuery = ""
	strippedURL.ForceQuery = false
	strippedURL.Fragment = ""
	strippedURL.RawFragment = ""
	fetchURL := strippedURL.String()
	if outcome, seen := p.outcomes[fetchURL]; seen {
		return outcome
	}
	fmt.Printf("fetching %s\n", fetchURL)
	outcome := p.fetch(fetchURL)
	p.outcomes[fetchURL] = outcome
	return outcome
}

// fetch GETs the URL and returns either the failure reason or the id
// attribute values of the HTML document it resolves to.
func (p *prober) fetch(fetchURL string) probeOutcome {
	request, err := http.NewRequest(http.MethodGet, fetchURL, nil)
	if err != nil {
		return probeOutcome{failure: fmt.Sprintf("building request: %v", err)}
	}
	request.Header.Set("User-Agent", probeUserAgent)
	response, err := p.client.Do(request)
	if err != nil {
		var wrapped *url.Error
		if errors.As(err, &wrapped) {
			err = wrapped.Err
		}
		return probeOutcome{failure: fmt.Sprintf("GET failed: %v", err)}
	}
	defer func() {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK {
		return probeOutcome{failure: fmt.Sprintf("HTTP status %d, not 200", response.StatusCode)}
	}
	contentType := response.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "text/html" {
		return probeOutcome{failure: fmt.Sprintf("Content-Type %q is not text/html", contentType)}
	}
	documentRoot, err := html.Parse(response.Body)
	if err != nil {
		return probeOutcome{failure: fmt.Sprintf("parsing HTML: %v", err)}
	}
	return probeOutcome{documentIDs: elementIDs(documentRoot)}
}

// elementIDs collects the value of every id attribute in the document - the
// targets a URL fragment can resolve to.
func elementIDs(documentRoot *html.Node) map[string]struct{} {
	ids := make(map[string]struct{})
	for node := range documentRoot.Descendants() {
		if node.Type != html.ElementNode {
			continue
		}
		for _, attribute := range node.Attr {
			if attribute.Key == "id" {
				ids[attribute.Val] = struct{}{}
			}
		}
	}
	return ids
}
