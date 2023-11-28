# Release instructions

This document outlines the neofs-contract release process. It can be used as a
todo list for a new release.

## Check the state

These should run successfully:
 * build
 * unit-tests
 * lint

## Update CHANGELOG

Add an entry to the CHANGELOG.md following the style established there.

## Update versions

Ensure VERSION and common/version.go files contain the proper target version,
update if needed.

Create a PR with CHANGELOG/version changes, review/merge it.

## Create a GitHub release and a tag

Use "Draft a new release" button in the "Releases" section. Create a new
`vX.Y.Z` tag for it following the semantic versioning standard. Put change log
for this release into the description. Do not attach any binaries at this step.
Set the "Set as the latest release" checkbox if this is the latest stable
release or "Set as a pre-release" if this is an unstable pre-release.
Press the "Publish release" button.

## Add neofs-contract tarball

Fetch the new tag from the GitHub, do `make mr_proper && make archive` locally.
It should produce neofs-contract-vX.Y.Z.tar.gz tarball.

## Close GitHub milestone

Close corresponding X.Y.Z GitHub milestone.

## Deployment

Update NeoFS node with the new contracts.
