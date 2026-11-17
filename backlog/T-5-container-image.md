---
id: T-5
type: Task
title: "Publish the container image"
status: offen
milestone: M-4
tags: [release, container]
created: 2026-11-17
---

The `Dockerfile` and `scripts/check-image.sh` exist and the CI checks the image, but nothing publishes it: the
release workflow only attaches the binaries. A workflow (or a job of `release.yml`) should build the image for the
supported platforms in the tag run, check it with `scripts/check-image.sh`, sign it and push it to a registry, with
the digest named in the release notes.

Done when a tag publishes a checked, signed image and `compose.yaml` can name it instead of `build: .`. Deferred on
purpose: nothing is published before the first release.
