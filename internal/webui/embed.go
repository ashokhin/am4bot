// Package webui embeds the built React frontend (web/dist) into the
// apiserver binary, so one process serves both the UI and the API --
// no separate nginx/web container needed. See
// docs/multi-tenant-hosting.md's Architecture section.
//
// dist/ here is populated by the Docker build (COPY --from=web-build
// web/dist internal/webui/dist, see Dockerfile.controlplane) before `go
// build` runs -- go:embed can't reach outside this package's own
// directory tree, so the frontend build output has to land here rather
// than being embedded straight from web/dist. Outside Docker, dist/
// holds only the checked-in placeholder unless you've built the
// frontend yourself and copied it in -- fine for `go build`/`go vet`/
// tests, none of which actually serve the UI.
package webui

import "embed"

//go:embed all:dist
var DistFS embed.FS
