// Package openapi holds the OpenAPI description of ferry's HTTP API,
// embedded, so the service can serve the very file its code was generated
// from.
package openapi

import _ "embed"

// V1 is api/openapi/ferry-v1.yaml as written.
//
//go:embed ferry-v1.yaml
var V1 []byte
