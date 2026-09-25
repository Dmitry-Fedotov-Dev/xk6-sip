// Command export copies the k6/x/sip reference to a grafana/k6-docs checkout,
// converting it from GitHub Markdown to the k6-docs Hugo format: links to
// .md files become page URLs and examples are wrapped in {{< code >}}.
//
//	go run ./docs/export ../k6-docs/docs/sources/k6/next/javascript-api
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const module = "k6-x-sip"

var (
	mdLink  = regexp.MustCompile(`\]\(([^)#:]+\.md)(#[^)]*)?\)`)
	example = regexp.MustCompile("(?s)<!-- md-k6:skip -->\n\n```javascript\n.*?\n```\n")
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./docs/export <k6-docs>/docs/sources/k6/next/javascript-api")
		os.Exit(2)
	}
	src := filepath.Join("docs", "sources", "k6", "next", "javascript-api", module)
	dst := filepath.Join(os.Args[1], module)
	n := 0
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p) // #nosec G304 -- walking our own docs tree
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
			return err
		}
		n++
		// #nosec G306 -- documentation meant to be committed and read by others
		return os.WriteFile(out, []byte(convert(filepath.ToSlash(rel), string(data))), 0o644)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("exported %d pages to %s\n", n, dst)
}

// convert turns one page from GitHub Markdown into k6-docs Hugo Markdown.
func convert(rel, s string) string {
	s = mdLink.ReplaceAllStringFunc(s, func(m string) string {
		sub := mdLink.FindStringSubmatch(m)
		target := path.Join(path.Dir(rel), sub[1])
		return "](" + relURL(pageURL(rel), pageURL(target)) + sub[2] + ")"
	})
	return example.ReplaceAllStringFunc(s, func(m string) string {
		return "{{< code >}}\n\n" + m + "\n{{< /code >}}\n"
	})
}

// pageURL is the Hugo URL of a page relative to the module root:
// "call/trace.md" -> "call/trace/", "device/_index.md" -> "device/".
func pageURL(file string) string {
	if path.Base(file) == "_index.md" {
		if d := path.Dir(file); d != "." {
			return d + "/"
		}
		return ""
	}
	return strings.TrimSuffix(file, ".md") + "/"
}

// relURL is the relative link from page URL "from" to page URL "to".
func relURL(from, to string) string {
	f := strings.Split(strings.TrimSuffix(from, "/"), "/")
	t := strings.Split(strings.TrimSuffix(to, "/"), "/")
	if from == "" {
		f = nil
	}
	if to == "" {
		t = nil
	}
	i := 0
	for i < len(f) && i < len(t) && f[i] == t[i] {
		i++
	}
	up := strings.Repeat("../", len(f)-i)
	down := strings.Join(t[i:], "/")
	if down != "" {
		down += "/"
	}
	if up+down == "" {
		return "./"
	}
	return up + down
}
