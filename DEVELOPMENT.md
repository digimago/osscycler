# Developing osscycler

Everything here is run from the repository root with `make`; `make help`
lists the targets. The design, the decisions behind it, the protocol
notes and the working conventions are in [CLAUDE.md](CLAUDE.md), the
notes osscycler's AI-assisted development works from (see the README).
Each package documents its own part: start with `go doc ./internal/telemetry`,
`go doc ./internal/ride` and `go doc ./internal/api`, and the gRPC schema in
`proto/osscycler/v1/telemetry.proto`.

## Setup

You need Go 1.27 or newer. Code generation (`buf`) and the linters run
through `go run` / `go tool`, so there is nothing else to install.

```sh
make help            # list targets
make udev            # once: stick permissions (sudo), then replug and log in again
make stack-fake      # core with a synthetic ride + TUI, no hardware needed
make stack           # core on the ANT stick + TUI; quit the TUI with q
make demo            # synthetic rider, demo course and workouts, guided tour
```

`make core` and `make tui` in two terminals show the core's log live; via
`make stack` it goes to `_logs/core.log`.

## The ANT+ network key

Release binaries have the ANT+ network key built in; a build from source
needs it in `ANT_PLUS_NETWORK_KEY` or `_secrets/ant-network-key` (mode
600). Accept the ANT+ Adopter Agreement at
<https://developer.garmin.com/ant-program/downloads/> and copy the ANT+ key
from <https://developer.garmin.com/ant-program/ant-ant-plus/network-keys/>.
`osscycler-core -key-check` says where the key comes from without
printing it.

The key must never be committed: a test fails if any file git would
commit contains it, in any formatting. It runs wherever the key is known
(your key file, the CI secret).

## Local settings

Directories starting with `_` are local only and git-ignored: `_bin/`
(binaries), `_logs/`, `_secrets/` (API token, ANT+ key), `_gpx/` (your
courses), `_workouts/`, `_rides/` (recordings), `_demo/`, `_dist/`
(releases), `_scratch/` (throwaway programs).

Run through `make`, the core uses these `_` folders and `_secrets/api-token`
(created on first use) instead of the rider's `~/osscycler`, except for
the profile, which is the real one (`~/osscycler/profile.json`). Run by
hand, set `OSSCYCLER_HOME` to a scratch folder to keep a test away from
your own rides and settings: a chosen folder never takes over data from
elsewhere. Personal make settings go in a git-ignored `local.mk`:

```make
# Force settings for a test run; they override the rider profile and are
# never saved.
CORE_FLAGS = -rider-kg 75 -rawlog ride.rawlog
# Push metrics over OTLP/HTTP, e.g. to Prometheus 3 with its OTLP receiver.
export OTEL_EXPORTER_OTLP_METRICS_ENDPOINT = http://prometheus:9090/api/v1/otlp/v1/metrics
```

The rider's real settings live in the profile, set through onboarding;
the demo uses its own in `_demo/`.

## Branches

- `develop` is where work goes, and GitHub's default branch: open pull
  requests against it. It takes a change once the `check` workflow has
  passed on it.
- `main` holds releases only. It takes a pull request from a
  `release/X.Y` or `hotfix/X.Y.Z` branch of this repository, merged with
  a merge commit (not squashed or rebased), once `check` and
  `release-branch` have passed.
- Neither can be deleted or force-pushed, and release tags (`v*`) can't
  be moved or deleted.

A release, step by step:

```sh
git switch -c release/0.3 origin/develop     # notes/v0.3.0.md, last fixes
git push -u origin release/0.3               # then a pull request into main
# merge it with a merge commit, then tag that merge on main:
git switch main && git pull
git tag -a v0.3.0 -m "osscycler 0.3.0" && git push origin v0.3.0
# check the draft release on GitHub, then publish it
git switch develop && git pull && git merge --ff-only main && git push
```

Bringing `develop` up to `main` afterwards puts the tag in its history,
so `git describe` (and the version of builds from `develop`) moves on. A
hotfix goes the same way from `main` on a `hotfix/0.3.1` branch, into
`main` and then into `develop`.

## Checks

`make check` runs gofmt, vet, staticcheck and the race-enabled tests;
run it before committing. After editing `proto/`, run `make generate`
(the generated code in `gen/` is committed).

`make replay` re-rides the course rides in `_rides/` through the
simulation and compares the times with the recorded ones; add
`REPLAY_FLAGS="-cda 0.25"` (or `-mass-kg`, `-crr`) to see what other
parameters would have done.

## Releases

`make release` (`scripts/release.sh`) builds into `_dist/`, with the ANT+
key from `ANT_PLUS_NETWORK_KEY` or the key file built in:

- Linux amd64 and arm64: `.tar.gz`, `.deb` and `.rpm` (packaging in
  `deploy/packaging/`). Both carry the udev rule and a systemd unit
  (`deploy/systemd/osscycler@.service`, runs the core as a given user);
  the archive also has `install-udev.sh`. The packages were checked by
  installing them in Debian and Fedora containers, the unit under a real
  systemd (privileged Fedora container).
- macOS: a universal `.tar.gz` (Intel and Apple Silicon)
- `SHA256SUMS`

Every pull request, and every push to `develop` and `main`, runs `make
check`, the 3D view's tests and actionlint in GitHub Actions (the `check`
workflow), without secrets: the network key test skips there.

Pushing a `v*` tag runs the network key test and `make release` in
GitHub Actions with the repository secret `ANT_PLUS_NETWORK_KEY` (the
rest of the checks ran on that commit when it reached `main`) and makes a draft
release with every file attached. Check it (files, notes), then publish
it on GitHub: published releases are immutable, so a fix after that is a
new version. Running the workflow by hand builds the same files as
workflow artifacts, without a release.
