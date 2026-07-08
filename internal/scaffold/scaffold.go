// Package scaffold fetches a template branch and materializes a new project.
package scaffold

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Options controls a single scaffold run.
type Options struct {
	Name    string
	Lang    string
	Variant string
	Dir     string
	Ref     string
	Owner   string
	Repo    string
	Sets    map[string]string
}

type varSpec struct {
	Prompt      string `json:"prompt"`
	Default     string `json:"default"`
	WhenLang    string `json:"when_lang"`
	WhenVariant string `json:"when_variant"`
}

type step struct {
	Cmd         string `json:"cmd"`
	WhenLang    string `json:"when_lang"`
	WhenVariant string `json:"when_variant"`
}

type manifest struct {
	Exclude    []string           `json:"exclude"`
	Vars       map[string]varSpec `json:"vars"`
	Substitute []string           `json:"substitute"`
	PostCreate []step             `json:"post_create"`
}

// Run scaffolds a new project into <Dir>/<Name>.
func Run(o Options) error {
	ref := resolveRef(o)
	fmt.Printf("Fetching %s/%s@%s ...\n", o.Owner, o.Repo, ref)
	src, err := fetch(o.Owner, o.Repo, "refs/heads/"+ref)
	if err != nil {
		return fmt.Errorf("%w\n(run `ainpt list-langs` to see available templates)", err)
	}
	defer os.RemoveAll(src)

	m := loadManifest(src)

	target := filepath.Join(o.Dir, o.Name)
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("target %q already exists", target)
	}

	vars, err := resolveVars(m, o)
	if err != nil {
		return err
	}

	if err := copyTree(src, target, m.Exclude); err != nil {
		return err
	}
	if err := substitute(target, m.Substitute, vars); err != nil {
		return err
	}
	if err := runSteps(target, m.PostCreate, vars, o.Lang, o.Variant); err != nil {
		return err
	}

	writeLock(target, o, ref, vars)

	fmt.Printf("\nCreated %s\n", target)
	return nil
}

func resolveRef(o Options) string {
	switch {
	case o.Ref != "":
		return o.Ref
	case o.Lang != "" && o.Variant != "":
		return "lang/" + o.Lang + "/" + o.Variant
	case o.Lang != "":
		return "lang/" + o.Lang
	default:
		return "main"
	}
}

// fetch downloads and extracts the branch tarball into a temp dir, stripping
// the top-level "<repo>-<ref>/" wrapper that GitHub adds. Returns the temp root.
func fetch(owner, repo, refPath string) (string, error) {
	url := fmt.Sprintf("https://codeload.github.com/%s/%s/tar.gz/%s", owner, repo, refPath)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %s", url, resp.Status)
	}

	tmp, err := os.MkdirTemp("", "ainpt-*")
	if err != nil {
		return "", err
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return "", err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		rel := stripFirst(h.Name)
		if rel == "" {
			continue
		}
		dst := filepath.Join(tmp, rel)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return "", err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return "", err
			}
			f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode))
			if err != nil {
				return "", err
			}
			if _, err := io.Copy(f, tr); err != nil { //nolint:gosec // trusted source
				f.Close()
				return "", err
			}
			f.Close()
		}
	}
	return tmp, nil
}

func stripFirst(name string) string {
	name = strings.TrimPrefix(name, "./")
	i := strings.Index(name, "/")
	if i < 0 {
		return ""
	}
	return name[i+1:]
}

func loadManifest(root string) manifest {
	m := manifest{}
	if b, err := os.ReadFile(filepath.Join(root, "template.json")); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	// These are never copied into a scaffolded project.
	m.Exclude = append(m.Exclude, ".git", "template.json")
	return m
}

func resolveVars(m manifest, o Options) (map[string]string, error) {
	vars := map[string]string{"name": o.Name}
	for k, v := range o.Sets {
		vars[k] = v
	}
	if _, ok := vars["PROJECT_NAME"]; !ok {
		vars["PROJECT_NAME"] = o.Name
	}
	for key, spec := range m.Vars {
		if spec.WhenLang != "" && spec.WhenLang != o.Lang {
			continue
		}
		if spec.WhenVariant != "" && spec.WhenVariant != o.Variant {
			continue
		}
		if _, ok := vars[key]; ok {
			continue
		}
		if spec.Default != "" {
			vars[key] = expand(spec.Default, vars)
			continue
		}
		return nil, fmt.Errorf("missing required variable %q (pass --set %s=VALUE)", key, key)
	}
	return vars, nil
}

func expand(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
	}
	return s
}

func excluded(rel string, patterns []string) bool {
	rel = filepath.ToSlash(rel)
	for _, p := range patterns {
		p = strings.TrimSuffix(filepath.ToSlash(p), "/")
		if p == "" {
			continue
		}
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
		if ok, _ := filepath.Match(p, rel); ok {
			return true
		}
	}
	return false
}

func copyTree(src, dst string, excl []string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if excluded(rel, excl) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func substitute(root string, files []string, vars map[string]string) error {
	for _, f := range files {
		p := filepath.Join(root, f)
		b, err := os.ReadFile(p)
		if err != nil {
			continue // listed but absent — skip quietly
		}
		if err := os.WriteFile(p, []byte(expand(string(b), vars)), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func runSteps(dir string, steps []step, vars map[string]string, lang, variant string) error {
	for _, s := range steps {
		if s.WhenLang != "" && s.WhenLang != lang {
			continue
		}
		if s.WhenVariant != "" && s.WhenVariant != variant {
			continue
		}
		cmd := expand(s.Cmd, vars)
		fmt.Printf("  post-create: %s\n", cmd)
		c := exec.Command("sh", "-c", cmd)
		c.Dir = dir
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("post_create %q: %w", cmd, err)
		}
	}
	return nil
}

// Lock records where a project came from so `ainpt update` can 3-way merge
// later template changes. Written as .ainpt.json in the project root.
type Lock struct {
	Template string            `json:"template"`
	Ref      string            `json:"ref"`
	Lang     string            `json:"lang,omitempty"`
	Variant  string            `json:"variant,omitempty"`
	Commit   string            `json:"commit"`
	Vars     map[string]string `json:"vars,omitempty"`
}

func writeLock(target string, o Options, ref string, vars map[string]string) {
	sha, err := resolveSHA(o.Owner, o.Repo, ref)
	if err != nil {
		fmt.Println("  warning: could not record template commit; `ainpt update` will need it set manually")
	}
	v := map[string]string{}
	for k, val := range vars {
		if k == "name" {
			continue
		}
		v[k] = val
	}
	lock := Lock{Template: o.Owner + "/" + o.Repo, Ref: ref, Lang: o.Lang, Variant: o.Variant, Commit: sha, Vars: v}
	b, _ := json.MarshalIndent(lock, "", "  ")
	_ = os.WriteFile(filepath.Join(target, ".ainpt.json"), append(b, '\n'), 0o644)
}

func resolveSHA(owner, repo, ref string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits/%s", owner, repo, ref)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.sha")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("resolve sha for %s: %s", ref, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// Update 3-way merges upstream template changes into an existing project.
func Update(dir string) error {
	if dir == "" {
		dir = "."
	}
	lb, err := os.ReadFile(filepath.Join(dir, ".ainpt.json"))
	if err != nil {
		return fmt.Errorf("no .ainpt.json in %q — was this project created by ainpt?", dir)
	}
	var lock Lock
	if err := json.Unmarshal(lb, &lock); err != nil {
		return fmt.Errorf("parse .ainpt.json: %w", err)
	}
	if lock.Commit == "" {
		return fmt.Errorf(".ainpt.json has no base commit; cannot 3-way merge")
	}
	parts := strings.SplitN(lock.Template, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid template %q in .ainpt.json", lock.Template)
	}
	owner, repo := parts[0], parts[1]

	newSHA, err := resolveSHA(owner, repo, lock.Ref)
	if err != nil {
		return err
	}
	if newSHA == lock.Commit {
		fmt.Println("Already up to date.")
		return nil
	}
	fmt.Printf("Updating %s@%s: %s -> %s\n", lock.Template, lock.Ref, short(lock.Commit), short(newSHA))

	oldSrc, err := fetch(owner, repo, lock.Commit)
	if err != nil {
		return err
	}
	defer os.RemoveAll(oldSrc)
	newSrc, err := fetch(owner, repo, "refs/heads/"+lock.Ref)
	if err != nil {
		return err
	}
	defer os.RemoveAll(newSrc)

	m := loadManifest(newSrc)

	var added, merged int
	var conflicts []string

	err = filepath.Walk(newSrc, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(newSrc, path)
		if err != nil {
			return err
		}
		if rel == "." || info.IsDir() {
			return nil
		}
		if excluded(rel, m.Exclude) {
			return nil
		}
		mine := filepath.Join(dir, rel)
		if _, err := os.Stat(mine); os.IsNotExist(err) {
			// New upstream file — add it verbatim.
			if err := os.MkdirAll(filepath.Dir(mine), 0o755); err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if err := os.WriteFile(mine, data, info.Mode()); err != nil {
				return err
			}
			added++
			return nil
		}
		base := filepath.Join(oldSrc, rel)
		if _, err := os.Stat(base); os.IsNotExist(err) {
			base = os.DevNull
		}
		// In-place 3-way merge: keep local edits, fold in the upstream delta.
		c := exec.Command("git", "merge-file",
			"-L", "yours", "-L", "template (old)", "-L", "template (new)",
			mine, base, path)
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			if _, ok := err.(*exec.ExitError); ok {
				conflicts = append(conflicts, rel)
			} else {
				return fmt.Errorf("merge %s: %w", rel, err)
			}
		}
		merged++
		return nil
	})
	if err != nil {
		return err
	}

	lock.Commit = newSHA
	if b, err := json.MarshalIndent(lock, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(dir, ".ainpt.json"), append(b, '\n'), 0o644)
	}

	fmt.Printf("\nMerged %d file(s), added %d new file(s).\n", merged, added)
	if len(conflicts) > 0 {
		fmt.Printf("%d file(s) have conflicts to resolve:\n", len(conflicts))
		for _, c := range conflicts {
			fmt.Printf("  %s\n", c)
		}
		fmt.Println("Resolve the <<<<<<< markers, then commit.")
	} else {
		fmt.Println("No conflicts. Review the diff and commit.")
	}
	return nil
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
