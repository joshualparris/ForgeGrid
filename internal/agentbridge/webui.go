package agentbridge

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed webui/*
var webUIFS embed.FS

// WebUIHandler returns an http.Handler serving the local AgentBridge GUI
// (login, inbox, composer). It's a plain static file server over the
// embedded assets - the page itself authenticates against the existing
// /api/v1/agent-* routes with a Bearer token, the same as any other
// AgentBridge client.
func WebUIHandler() (http.Handler, error) {
	sub, err := fs.Sub(webUIFS, "webui")
	if err != nil {
		return nil, err
	}
	return http.FileServer(http.FS(sub)), nil
}
