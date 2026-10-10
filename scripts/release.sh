#!/bin/sh
# release.sh — cut a release: bump plugin.json, promote the CHANGELOG
# [Unreleased] section to the new version, tag the commit, and push.
#
# Usage:
#   scripts/release.sh <version> [--dry-run] [--yes] [--no-test]
#   make release VERSION=X.Y.Z
#
# <version> is a bare semver ("X.Y.Z" or "vX.Y.Z"; a leading "v" is stripped).
# The script:
#   1. validates the version, branch, a clean tracked tree, and that the tag
#      does not already exist locally or on the remote;
#   2. runs the release quality gate (build, vet, gofmt, tests) — the same gate
#      as .github/workflows/release.yml — unless --no-test / SKIP_TESTS=1;
#   3. bumps "version" in plugin.json and rewrites the top "## [Unreleased]"
#      CHANGELOG heading into "## [<version>] - <YYYY-MM-DD>", reopening a
#      fresh [Unreleased] above it;
#   4. commits those two files, creates an annotated tag v<version>, and pushes
#      the branch and tag to the remote.
#
# Pushing the tag triggers the Release workflow (build + publish) on CI.
#
# Environment overrides:
#   REMOTE=origin   git remote to push to
#   BRANCH=main     branch to release from
#   SKIP_TESTS=1    skip the quality gate
#   DRY_RUN=1       print the plan without changing anything
#
# Untracked files do not block a release (only staged/unstaged changes do).
# POSIX sh, for macOS/Linux. Not runnable from the Windows dev shell (same
# caveat as scripts/check-stale-commands.sh).
set -eu

usage() {
	cat <<'EOF'
usage: scripts/release.sh <version> [--dry-run] [--yes] [--no-test]

  <version>     bare semver to release, e.g. X.Y.Z (a leading "v" is stripped)
  --dry-run     show what would change without writing, committing, or pushing
  -y, --yes     do not prompt before pushing
  --no-test     skip the build/vet/gofmt/test gate (CI still gates the tag)
  -h, --help    show this help
EOF
}

VERSION=""
DRY_RUN="${DRY_RUN:-0}"
ASSUME_YES=0
SKIP_TESTS="${SKIP_TESTS:-0}"

for arg in "$@"; do
	case "$arg" in
		--dry-run) DRY_RUN=1 ;;
		-y | --yes) ASSUME_YES=1 ;;
		--no-test | --skip-tests) SKIP_TESTS=1 ;;
		-h | --help)
			usage
			exit 0
			;;
		-*)
			echo "release: unknown option: $arg" >&2
			usage >&2
			exit 2
			;;
		*)
			if [ -n "$VERSION" ]; then
				echo "release: unexpected extra argument: $arg" >&2
				usage >&2
				exit 2
			fi
			VERSION="$arg"
			;;
	esac
done

if [ -z "$VERSION" ]; then
	echo "release: a version is required" >&2
	usage >&2
	exit 2
fi

VERSION="${VERSION#v}"
if ! printf '%s' "$VERSION" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$'; then
	echo "release: version must look like x.y.z (got '$VERSION')" >&2
	exit 2
fi

REMOTE="${REMOTE:-origin}"
BRANCH="${BRANCH:-main}"
TAG="v$VERSION"
TODAY="$(date +%Y-%m-%d)"

# SC1007: "CDPATH= cd" is the canonical idiom that clears CDPATH for the cd,
# preventing an inherited CDPATH from redirecting the path (same as
# scripts/check-stale-commands.sh).
# shellcheck disable=SC1007
repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$repo_root"
plugins="$repo_root/plugin.json"
changelog="$repo_root/CHANGELOG.md"

command -v git >/dev/null 2>&1 || {
	echo "release: git not found on PATH" >&2
	exit 1
}
[ -f "$plugins" ] || {
	echo "release: $plugins not found" >&2
	exit 1
}
[ -f "$changelog" ] || {
	echo "release: $changelog not found" >&2
	exit 1
}

current_branch="$(git rev-parse --abbrev-ref HEAD)"
if [ "$current_branch" != "$BRANCH" ]; then
	echo "release: on branch '$current_branch', expected '$BRANCH' (override with BRANCH=...)" >&2
	exit 1
fi

if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
	echo "release: tracked files have staged/unstaged changes; commit or stash first" >&2
	git status --short --untracked-files=no >&2
	exit 1
fi

if git rev-parse -q --verify "refs/tags/$TAG" >/dev/null; then
	echo "release: local tag $TAG already exists" >&2
	exit 1
fi
if git ls-remote --exit-code --tags "$REMOTE" "refs/tags/$TAG" >/dev/null 2>&1; then
	echo "release: tag $TAG already exists on $REMOTE" >&2
	exit 1
fi

# The top [Unreleased] section must contain at least one entry bullet.
unreleased_body="$(awk '
	/^## \[Unreleased\]/ { insec = 1; next }
	/^## / { insec = 0 }
	insec { print }
' "$changelog")"
if ! printf '%s\n' "$unreleased_body" | grep -q '^[[:space:]]*- '; then
	echo "release: CHANGELOG.md [Unreleased] has no entries (expected '- ' bullets)" >&2
	exit 1
fi

# promote_changelog rewrites the top [Unreleased] heading into the release
# heading and reopens an empty [Unreleased] above it. The entries that were
# under [Unreleased] now belong to the released version.
promote_changelog() {
	tmp="$(mktemp)"
	awk -v ver="$VERSION" -v day="$TODAY" '
		!done && /^## \[Unreleased\]/ {
			print "## [Unreleased]"
			print ""
			print "## [" ver "] - " day
			done = 1
			next
		}
		{ print }
	' "$changelog" >"$tmp"
	cp "$tmp" "$changelog"
	rm -f "$tmp"
}

bump_plugin() {
	tmp="$(mktemp)"
	sed -E "s/(\"version\"[[:space:]]*:[[:space:]]*\")[^\"]*(\")/\1${VERSION}\2/" "$plugins" >"$tmp"
	cp "$tmp" "$plugins"
	rm -f "$tmp"
}

run_gate() {
	echo "release: quality gate — build, vet, gofmt, test" >&2
	CGO_ENABLED=1 go build -tags "fts5 sqlite_fts5" -o /dev/null .
	CGO_ENABLED=1 go vet -tags "fts5 sqlite_fts5" ./...
	if [ -n "$(gofmt -l cmd internal main.go)" ]; then
		echo "release: gofmt reported unformatted files:" >&2
		gofmt -l cmd internal main.go >&2
		return 1
	fi
	CGO_ENABLED=1 go test -tags "fts5 sqlite_fts5" ./...
}

if [ "$SKIP_TESTS" != "1" ]; then
	run_gate
else
	echo "release: skipping quality gate (SKIP_TESTS=1)" >&2
fi

if [ "$DRY_RUN" = "1" ]; then
	echo "release: DRY RUN — nothing written"
	echo "  would bump $plugins to version $VERSION"
	echo "  would add '## [$VERSION] - $TODAY' to CHANGELOG.md and reopen [Unreleased]"
	echo "  would commit 'release $TAG', tag $TAG, and push to $REMOTE/$BRANCH"
	exit 0
fi

bump_plugin
promote_changelog

git add "$plugins" "$changelog"
git commit -m "release $TAG"
git tag -a "$TAG" -m "release $TAG"

if [ "$ASSUME_YES" != "1" ]; then
	printf 'release: push %s and tag %s to %s? [y/N] ' "$BRANCH" "$TAG" "$REMOTE" >&2
	reply=""
	if [ -r /dev/tty ]; then
		read -r reply </dev/tty || reply=""
	else
		read -r reply || reply=""
	fi
	case "$reply" in
		y | Y | yes | YES) ;;
		*)
			echo "release: aborted before push. Local commit and tag remain; undo with:" >&2
			echo "  git tag -d $TAG && git reset --hard HEAD~1" >&2
			exit 1
			;;
	esac
fi

git push "$REMOTE" "$BRANCH"
git push "$REMOTE" "$TAG"
echo "release: pushed $TAG — the Release workflow will build and publish it."
