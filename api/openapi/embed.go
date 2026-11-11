// Package openapi embeds the published Mobix HTTP contract.
package openapi

import _ "embed"

//go:embed mobix.json
var Specification []byte
