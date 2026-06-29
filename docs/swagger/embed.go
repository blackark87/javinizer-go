package swagger

import (
	_ "embed"
	"encoding/json"
)

// swaggerJSON embeds the OpenAPI spec so the binary serves it without
// depending on a filesystem path (fixes Windows where CWD != exe dir).
//
//go:embed swagger.json
var swaggerJSON []byte

func init() {
	if !json.Valid(swaggerJSON) {
		panic("swagger: embedded swagger.json is not valid JSON")
	}
}

// SwaggerJSON returns a copy of the embedded OpenAPI specification.
func SwaggerJSON() []byte {
	cp := make([]byte, len(swaggerJSON))
	copy(cp, swaggerJSON)
	return cp
}
