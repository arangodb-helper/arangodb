# Maintainer Instructions

## Running tests

A subset of all tests are run in CI pipeline.

To run the entire test set, set the following environment variables:

  - `ARANGODB`: name of Docker image for ArangoDB to use
  - `VERBOSE` (optional): set to `1` for more output
  - `IP` (for Docker tests): set to a valid IP address on the local machine
  - `TESTOPTIONS`` (optional): set to `-test.run=REGEXPTOMATCHTESTS` to
    run only some tests

Then run:
```bash
make run-tests
```

To run only unit tests, execute:
```bash
make run-unit-tests
```

## Preparing a release

To prepare for a release, do the following:

- Update CHANGELOG.md. Update the first title (master -> new version). Commit it.
- Make sure all tests are OK.

## Building a release

On Linux, make sure that neither `.gobuild/tmp` nor `bin` contains any
files which are owned by `root`. For example, a `chmod -R` with your
user account is enough. This does not seem to be necessary on OSX.

To make a release you must have:

- A github access token in `~/.arangodb/github-token` that has read/write access
  for this repository.
- Push permission for the current docker account (`docker login <your-docker-hub-account>`)
  for the `arangodb` docker hub namespace.
- The latest checked out `master` branch of this repository.

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

## Completing after a release

After the release has been built (which includes publication) the following
has to be done:

- Update CHANGELOG.md. Add a new first title (released version -> master). Commit and push it.
