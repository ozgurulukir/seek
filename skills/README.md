# Installing the seek skill

The plugin packages the `skills/seek` skill, its references, helper scripts,
and standalone copies of optional Python services under
`skills/seek/scripts/services/`. `tools/` is the canonical source; after
changing a service, refresh its skill copy with `make skill-services`. These
targets use `uv` to provide Python 3.11; override `PYTHON_RUN` if you prefer
an existing interpreter. Run `make skill-services-check` to detect drift. The
`seek` binary itself must be installed separately and available in `PATH`.

## Codex

Add the repository marketplace, then install the plugin from the Codex plugin
directory:

```sh
codex plugin marketplace add ozgurulukir/seek
```

Open the Plugins Directory, select **seek-plugins**, and install **seek**.

## Claude Code

```sh
claude plugin marketplace add ozgurulukir/seek
claude plugin install seek@seek-plugins
```

## Python services

The optional semantic tagger is bundled in
`skills/seek/scripts/services/semantic/`, alongside its Python setup and model
bootstrap scripts. It is not required for keyword or hybrid search. The
package also includes the rich document extractor, reranker, and embedding/OCR
helpers under `scripts/services/`. Follow
[the semantic service reference](seek/references/semantic.md) to configure the
tagger. The setup scripts accept `SEEK_SEMANTIC_HOME` so the agent can keep
its virtual environment outside installed skill files while running the
bundled service.
