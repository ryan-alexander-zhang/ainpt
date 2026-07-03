// Command ainpt scaffolds a new project from the ai-native-project-template.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/ryan-alexander-zhang/ainpt/internal/scaffold"
)

// Injected at build time via -ldflags (see .goreleaser.yaml).
var (
	version = "dev"
	owner   = "ryan-alexander-zhang"
	repo    = "ai-native-project-template"
)

func init() {
	if v := os.Getenv("AINPT_OWNER"); v != "" {
		owner = v
	}
	if v := os.Getenv("AINPT_REPO"); v != "" {
		repo = v
	}
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "new":
		cmdNew(os.Args[2:])
	case "update":
		cmdUpdate(os.Args[2:])
	case "list-langs":
		cmdLangs()
	case "version", "-v", "--version":
		fmt.Println("ainpt", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`ainpt — scaffold a project from the ai-native-project-template

Usage:
  ainpt new <name> [--lang go] [--dir .] [--ref <branch>] [--set KEY=VALUE]
  ainpt update [--dir .]
  ainpt list-langs
  ainpt version

Flags for "new":
  --lang   language branch to use (lang/<lang>); empty uses the base template (main)
  --dir    parent directory for the new project (default ".")
  --ref    branch override (default: main, or lang/<lang> when --lang is set)
  --set    set a template variable, repeatable (e.g. --set MODULE_PATH=example.com/x)

"update" 3-way merges later template changes into an existing project (using the
.ainpt.json written at creation). Resolve any conflict markers, then commit.

Environment:
  AINPT_OWNER, AINPT_REPO   override the template source repository
`)
}

type setFlag map[string]string

func (s setFlag) String() string { return "" }
func (s setFlag) Set(v string) error {
	i := strings.Index(v, "=")
	if i < 0 {
		return fmt.Errorf("expected KEY=VALUE, got %q", v)
	}
	s[v[:i]] = v[i+1:]
	return nil
}

func cmdNew(args []string) {
	fs := flag.NewFlagSet("new", flag.ExitOnError)
	lang := fs.String("lang", "", "language branch (lang/<lang>); empty = base template")
	dir := fs.String("dir", ".", "parent directory for the new project")
	ref := fs.String("ref", "", "branch override (default: main or lang/<lang>)")
	sets := setFlag{}
	fs.Var(sets, "set", "set a template variable KEY=VALUE (repeatable)")

	// The stdlib flag package stops at the first positional, so parse in two
	// passes to accept flags both before and after <name>.
	_ = fs.Parse(args)
	rest := fs.Args()
	if len(rest) < 1 {
		fmt.Fprintln(os.Stderr, "usage: ainpt new <name> [--lang go] [--dir .] [--set K=V]")
		os.Exit(1)
	}
	name := rest[0]
	_ = fs.Parse(rest[1:])

	err := scaffold.Run(scaffold.Options{
		Name:  name,
		Lang:  *lang,
		Dir:   *dir,
		Ref:   *ref,
		Owner: owner,
		Repo:  repo,
		Sets:  sets,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func cmdUpdate(args []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	dir := fs.String("dir", ".", "project directory to update")
	_ = fs.Parse(args)
	if err := scaffold.Update(*dir); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func cmdLangs() {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/branches?per_page=100", owner, repo)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "error: %s returned %s\n", url, resp.Status)
		os.Exit(1)
	}
	var branches []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&branches); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Println("Available templates:")
	fmt.Println("  (default)   base template (main)")
	found := false
	for _, b := range branches {
		if strings.HasPrefix(b.Name, "lang/") {
			fmt.Printf("  --lang %s\n", strings.TrimPrefix(b.Name, "lang/"))
			found = true
		}
	}
	if !found {
		fmt.Println("  (no lang/* branches yet — only the base template is available)")
	}
}
