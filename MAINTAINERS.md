# Maintainer Instructions

## Init

Before starting any actions on repo, please do 
```bash
make init
```

## Running tests

A subset of all tests are run in CI pipeline.

To run the entire test set, set the following environment variables:

  - `ARANGODB`: name of Docker image for ArangoDB to use
  - `VERBOSE` (optional): set to `1` for more output
  - `IP` (for Docker tests): set to a valid IP address on the local machine
  - `TESTOPTIONS` (optional): set to `-test.run=REGEXPTOMATCHTESTS` to
    run only some tests

Then run:
```bash
make run-tests
```

To run only unit tests, execute:
```bash
make run-unit-tests
```

## Building a release

On Linux, make sure that neither `.gobuild/tmp` nor `bin` contains any
files which are owned by `root`. For example, a `chmod -R` with your
user account is enough. This does not seem to be necessary on OSX.

To make a release you must have:

- A github access token in `~/.arangodb/github-token` that has read/write access
  for this repository.
- Push permission for the current docker account (`docker login -u <your-docker-hub-account>`)
  for the `arangodb` docker hub namespace.
- The latest checked out `master` branch of this repository.

Preparation steps:
1. `docker login -u <your-docker-hub-account>`
2. `make vendor`

To create preview:
```bash
make prerelease-patch
# or
make prerelease-minor
# or
make prerelease-major
```

To create final version:
```bash
make release-patch
# or
make release-minor
# or
make release-major
```

If successful, a new version will be:

- Built for Mac, Windows & Linux.
- Tagged in GitHub
- Uploaded as GitHub release
- Pushed as Docker image to Docker Hub
- `./VERSION` will be updated to a `+git` version (after the release process)

If the release process fails, it may leave:

- `./VERSION` uncommitted. To resolve, checkout `master` or edit it to
  the original value and commit to master.
- A git tag named `<major>.<minor>.<patch>` in your repository.
  To resolve remove it using `git tag -d ...`.
- A git tag named `<major>.<minor>.<patch>` in this repository in GitHub.
  To resolve remove it manually.

## Finalizing the release

After the release has been built (which includes publication) the following
has to be done:

- Update CHANGELOG.md. Add a new first title (released version -> master). Commit and push it.

## CircleCI (optional automation)

The [`.circleci/config.yml`](.circleci/config.yml) pipeline mirrors the manual flow above in outline. Build status and logs are available in the CircleCI UI (and any notifications configured on the project).

- **check_quick** workflow: `make init`, `make check`, `make binaries`, `make vulncheck` on every push.
- **ci** workflow: full `make run-tests` on a **machine** executor when the pipeline is for a **pull request**. Set **`ARANGO_LICENSE_KEY`** in a CircleCI context (e.g. `github`) if `arangodbImage` is an Enterprise image.
- **publish-release-starter** workflow: runs only when pipeline parameter **`publish`** matches one of `prerelease-patch`, `prerelease-minor`, `prerelease-major`, `release-patch`, `release-minor`, `release-major`. **Four jobs** (see [`.circleci/config.yml`](.circleci/config.yml)):
  1. **`release-starter`** (amd64 machine, `resource_class: large`): version bump, GitHub release, binaries; sets **`SKIP_DOCKER_PUSH=1`** so the release tool skips **`make docker-push-version`**, writes **`ci-released-version.txt`** in the repo dir, and **`persist_to_workspace`** passes that file to downstream jobs. Context **`github-release`** only (`RELEASER_GITHUB_TOKEN`). **No** Docker Hub login on this job.
  2. **`starter-docker-amd64`** and **`starter-docker-arm64`** — workflow names for two runs of the same parameterized job **`starter-docker-arch`** (`go_arch` + `resource_class`: **`large`** / **`arm.large`**). After **`checkout`** + **`attach_workspace`** (restore **`ci-released-version.txt`**), each job installs Go for that arch, syncs the branch to the tip commit **`release-starter`** pushed, logs into Docker Hub, and runs **`make docker-push-arch-amd64`** or **`make docker-push-arch-arm64`** (native build per platform). Context **`docker-hub`** (`DOCKER_HUB_USER`, `DOCKER_HUB_PASSWORD`). Pushes only internal tags **`arangodb/arangodb-starter:<version>-amd64`** and **`-arm64`**.
  3. **`starter-docker-manifest`** (after both arch jobs succeed): **`make docker-push-manifest`** runs **`docker buildx imagetools create`** so public tags (**`<version>`**, **`x.y`**, **`x`**, **`latest`** when not a `-preview` version) point at a **multi-arch manifest** (`docker pull` / `docker run` select amd64 vs arm64 by host or `--platform`).

  **Manual `make release-*` on your machine** (do **not** set **`SKIP_DOCKER_PUSH`**) still runs **`make docker-push-version`** in one step (multi-arch via local Docker buildx / setup). To simulate CI locally you would set **`SKIP_DOCKER_PUSH=1`** and run the three **`make docker-push-arch-*`** / **`docker-push-manifest`** targets yourself with **`RELEASE_VERSION`** set.

For a one-off manual release, follow **Building a release** above; CI is intended as an optional, repeatable shortcut with the same `make` targets.
