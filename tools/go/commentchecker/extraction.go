package main

import (
	"fmt"
	"go/ast"
	"go/doc/comment"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// urlOccurrence is one appearance of a URL in a comment, addressed by the
// file path and line number where the URL's text sits.
type urlOccurrence struct {
	url  string
	file string
	line int
}

// compareOccurrences orders occurrences by file path, then line, then URL.
func compareOccurrences(left, right urlOccurrence) int {
	if byFile := strings.Compare(left.file, right.file); byFile != 0 {
		return byFile
	}
	if byLine := left.line - right.line; byLine != 0 {
		return byLine
	}
	return strings.Compare(left.url, right.url)
}

// publicSourceFiles returns, in lexical walk order, the Go files under
// moduleDirectory that carry the module's public API surface: every .go file
// except test files, files under internal or testdata directories, files
// under directories whose name starts with "." or "_" and files belonging to
// nested modules (directories below the root holding their own go.mod).
func publicSourceFiles(moduleDirectory string) ([]string, error) {
	rootDirectory := filepath.Clean(moduleDirectory)
	var files []string
	err := filepath.WalkDir(rootDirectory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path == rootDirectory {
				return nil
			}
			name := entry.Name()
			if name == "internal" || name == "testdata" ||
				strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// extractURLOccurrences parses the file and returns every URL that
// go/doc/comment recognizes in the file's comments - bare URLs in text and
// the URLs of link definitions - each mapped to the line where its text
// appears.
func extractURLOccurrences(filePath string) ([]urlOccurrence, error) {
	fileSet := token.NewFileSet()
	parsedFile, err := parser.ParseFile(fileSet, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", filePath, err)
	}
	var occurrences []urlOccurrence
	var commentParser comment.Parser
	for _, group := range parsedFile.Comments {
		parsedComment := commentParser.Parse(group.Text())
		urls := make(map[string]struct{})
		collectBlockURLs(parsedComment.Content, urls)
		for _, linkDefinition := range parsedComment.Links {
			urls[linkDefinition.URL] = struct{}{}
		}
		occurrences = append(occurrences, locateInGroup(fileSet, group, urls)...)
	}
	return occurrences, nil
}

// collectBlockURLs adds to urls the target of every link in the given comment
// blocks, recursing through list items. Code blocks carry no links, and link
// definitions are collected separately by the caller.
func collectBlockURLs(blocks []comment.Block, urls map[string]struct{}) {
	for _, block := range blocks {
		switch typedBlock := block.(type) {
		case *comment.Paragraph:
			collectTextURLs(typedBlock.Text, urls)
		case *comment.Heading:
			collectTextURLs(typedBlock.Text, urls)
		case *comment.List:
			for _, item := range typedBlock.Items {
				collectBlockURLs(item.Content, urls)
			}
		}
	}
}

// collectTextURLs adds to urls the target of every link in the given text
// spans, covering both bare URLs and links resolved from link definitions.
func collectTextURLs(texts []comment.Text, urls map[string]struct{}) {
	for _, text := range texts {
		if link, ok := text.(*comment.Link); ok {
			urls[link.URL] = struct{}{}
		}
	}
}

// locateInGroup maps each URL to every place its text appears in the comment
// group. Longer URLs are located first and claim their text, so a URL that is
// a prefix of another never reports the longer URL's position. A URL whose
// text cannot be found at all is reported at the group's first line rather
// than dropped.
func locateInGroup(fileSet *token.FileSet, group *ast.CommentGroup, urls map[string]struct{}) []urlOccurrence {
	if len(urls) == 0 {
		return nil
	}
	sortedURLs := make([]string, 0, len(urls))
	for url := range urls {
		sortedURLs = append(sortedURLs, url)
	}
	slices.SortFunc(sortedURLs, func(left, right string) int {
		if byLength := len(right) - len(left); byLength != 0 {
			return byLength
		}
		return strings.Compare(left, right)
	})

	located := make(map[string]bool, len(sortedURLs))
	var occurrences []urlOccurrence
	for _, commentElement := range group.List {
		rawText := commentElement.Text
		elementPosition := fileSet.Position(commentElement.Pos())
		var claimedRanges [][2]int
		for _, url := range sortedURLs {
			searchFrom := 0
			for {
				foundAt := strings.Index(rawText[searchFrom:], url)
				if foundAt < 0 {
					break
				}
				start := searchFrom + foundAt
				end := start + len(url)
				searchFrom = end
				if overlapsAny(claimedRanges, start, end) {
					continue
				}
				claimedRanges = append(claimedRanges, [2]int{start, end})
				located[url] = true
				occurrences = append(occurrences, urlOccurrence{
					url:  url,
					file: elementPosition.Filename,
					line: elementPosition.Line + strings.Count(rawText[:start], "\n"),
				})
			}
		}
	}
	groupPosition := fileSet.Position(group.Pos())
	for _, url := range sortedURLs {
		if !located[url] {
			occurrences = append(occurrences, urlOccurrence{
				url:  url,
				file: groupPosition.Filename,
				line: groupPosition.Line,
			})
		}
	}
	slices.SortFunc(occurrences, compareOccurrences)
	return occurrences
}

// overlapsAny reports whether the half-open range [start, end) intersects any
// of the given ranges.
func overlapsAny(ranges [][2]int, start, end int) bool {
	for _, claimed := range ranges {
		if start < claimed[1] && claimed[0] < end {
			return true
		}
	}
	return false
}
