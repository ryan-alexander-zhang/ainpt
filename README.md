# ainpt

Scaffold a new project from the
[ai-native-project-template](https://github.com/ryan-alexander-zhang/ai-native-project-template).

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/ryan-alexander-zhang/ainpt/main/install.sh | sh
```

This downloads the right binary for your OS/arch from the latest GitHub release
and installs it to `/usr/local/bin` (or `~/.local/bin`).

## Usage

```bash
ainpt new my-project                        # base template (main branch)
ainpt new my-service --lang go              # Go template (lang/go branch)
ainpt new my-app --lang java --variant ddd  # variant (lang/java/ddd branch)
ainpt update                                # pull later template changes into this project
ainpt list-langs                            # show available language templates
ainpt version
```

## Updating a project

`ainpt new` records where the project came from in `.ainpt.json` (template repo,
ref, and the exact commit). Later, from inside the project:

```bash
ainpt update            # or: ainpt update --dir path/to/project
```

`update` fetches the template at the recorded commit (the base) and at the latest
ref, then does a per-file **3-way merge**: your local edits are kept, the upstream
delta is folded in, and any overlap is left as normal `<<<<<<<` conflict markers.
Only template-managed files are touched — your own files are never modified.
Resolve any conflicts and commit. (Requires `git` on PATH for the merge.)

Flags for `new`:

- `--lang <l>` — use the `lang/<l>` branch; omit for the base template
- `--variant <v>` — use the `lang/<l>/<v>` branch; requires `--lang`
- `--dir <path>` — parent directory for the new project (default `.`)
- `--ref <branch>` — branch override (default `main`, `lang/<l>`, or `lang/<l>/<v>`)
- `--set KEY=VALUE` — set a template variable (repeatable)

Override the source repo with `AINPT_OWNER` / `AINPT_REPO`.

## How it works

`ainpt` downloads the selected branch as a tarball, reads the template's
`template.json`, then copies the files (minus `exclude`d paths), substitutes
`{{VAR}}` placeholders, and runs the `post_create` steps (e.g. `git init`).
Adding a new language is just a new `lang/<l>` branch, and a new variant is a
`lang/<l>/<v>` branch — no change to this CLI.

## template.json

Each template branch describes its own scaffolding:

```json
{
  "exclude": [".github/"],
  "vars": {
    "MODULE_PATH": { "prompt": "Go module path", "when_lang": "go" }
  },
  "substitute": ["go.mod", "README.md"],
  "post_create": [
    { "cmd": "git init -q" },
    { "cmd": "git config core.hooksPath .githooks" },
    { "cmd": "go mod init {{MODULE_PATH}}", "when_lang": "go" }
  ]
}
```

`.git` and `template.json` are always excluded. `{{name}}` and `{{PROJECT_NAME}}`
are available by default.

### Conditioning on language and variant

Templates live on branches: `main` (base), `lang/<lang>`, and
`lang/<lang>/<variant>`. Each branch carries its own `template.json`. Inside a
variant branch, `vars` and `post_create` entries can be gated on the active
language and variant so one `template.json` covers the whole branch:

- `when_lang` — only applies when `--lang` matches (e.g. `"java"`)
- `when_variant` — only applies when `--variant` matches (e.g. `"ddd"`)

Both are exact matches; omit a field to always apply. Example for the
`lang/java/ddd` branch:

```json
{
  "exclude": [".github/"],
  "vars": {
    "GROUP_ID":    { "prompt": "Maven groupId",    "when_lang": "java" },
    "ARTIFACT_ID": { "prompt": "Maven artifactId", "when_lang": "java", "default": "{{name}}" }
  },
  "substitute": ["pom.xml", "README.md"],
  "post_create": [
    { "cmd": "git init -q" },
    { "cmd": "mvn -q -N wrapper:wrapper", "when_lang": "java" },
    { "cmd": "echo scaffolding DDD layers", "when_lang": "java", "when_variant": "ddd" }
  ]
}
```

## Release

Tag and push; GitHub Actions runs GoReleaser to build cross-platform binaries
and publish a release:

```bash
git tag v0.1.0 && git push origin v0.1.0
```
