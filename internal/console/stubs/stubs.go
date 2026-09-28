// Package stubs provides embedded code generator template files.
package stubs

import "embed"

// FS embeds all code generator stubs (*.tmpl).
//
//go:embed *.tmpl
var FS embed.FS
