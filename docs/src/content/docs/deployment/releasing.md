---
title: Releasing
description: How binaries and container images are published.
---

Releases are published by the [release workflow](https://github.com/sarathsp06/testhooks/blob/main/.github/workflows/release.yml) when a version tag is pushed:

```bash
git tag v1.1.0
git push origin v1.1.0
```

It then:

1. Builds the UI and cross-compiles the binary for Linux, macOS and Windows on amd64 and arm64, with the version from the tag embedded (`testhooks --version`).
2. Attaches the binaries and a `checksums.txt` to the GitHub release, creating the release with generated notes if it doesn't exist.
3. Builds and pushes a multi-arch image to `ghcr.io/sarathsp06/testhooks:<version>` and `:latest`.

The [installer script](../../getting-started/installation/#install-the-binary) and the [standalone Compose file](../../getting-started/installation/#docker-compose-with-postgresql) download from these.

To republish an existing tag, run the workflow manually from the Actions tab and enter the tag.

:::note[First release]
GitHub Container Registry makes a newly published package private. After the first release, open the package's settings on GitHub and set its visibility to public, or the Compose file won't be able to pull the image.
:::

## Documentation

This site is built with [Starlight](https://starlight.astro.build) from the `docs/` directory and deployed to GitHub Pages by the [docs workflow](https://github.com/sarathsp06/testhooks/blob/main/.github/workflows/docs.yml) on every push to `main` that touches `docs/`.

```bash
cd docs
npm ci
npm run dev       # http://localhost:4321/testhooks/
```
