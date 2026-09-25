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
	dst := filepath.Join(os.Args[1], module)
	n, err := export(filepath.Join("docs", "sources", "k6", "next", "javascript-api", module), dst)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("exported %d pages to %s\n", n, dst)
}

// export converts every page under src into dst. Both trees are accessed
// through os.Root, so no path can escape them.
func export(src, dst string) (int, error) {
	// #nosec G703 -- dst is the k6-docs checkout named by the user running the tool
	if err := os.MkdirAll(dst, 0o750); err != nil {
		return 0, err
	}
	in, err := os.OpenRoot(src)
	if err != nil {
		return 0, err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenRoot(dst)
	if err != nil {
		return 0, err
	}
	defer func() { _ = out.Close() }()

	n := 0
	err = fs.WalkDir(in.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		data, err := in.ReadFile(p)
		if err != nil {
			return err
		}
		if err := out.MkdirAll(path.Dir(p), 0o750); err != nil {
			return err
		}
		n++
		// #nosec G306 -- documentation meant to be committed and read by others
		return out.WriteFile(p, []byte(convert(p, string(data))), 0o644)
	})
	return n, err
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
