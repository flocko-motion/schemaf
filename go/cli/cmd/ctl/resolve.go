// Package ctl re-exports the compose resolver types from schemaf.local/base/compose
// for use by the CLI subcommands.
package ctl

import "schemaf.local/base/compose"

// Re-export types so existing callers in this package need no changes.
type SchemafExtension = compose.SchemafExtension
type HealthConfig = compose.HealthConfig
type ComposeFile = compose.ComposeFile
type ServiceDef = compose.ServiceDef

// Re-export functions.
var Resolve = compose.Resolve
var ShortNameMap = compose.ShortNameMap
var AllServices = compose.AllServices
var FilePaths = compose.FilePaths
var FindSchemafByService = compose.FindSchemafByService
var ContainerName = compose.ContainerName
