// Package ctl re-exports the compose resolver types from atlas.local/base/compose
// for use by the CLI subcommands.
package ctl

import "atlas.local/base/compose"

// Re-export types so existing callers in this package need no changes.
type AtlasExtension = compose.AtlasExtension
type HealthConfig = compose.HealthConfig
type ComposeFile = compose.ComposeFile
type ServiceDef = compose.ServiceDef

// Re-export functions.
var Resolve = compose.Resolve
var ShortNameMap = compose.ShortNameMap
var AllServices = compose.AllServices
var FilePaths = compose.FilePaths
var FindAtlasByService = compose.FindAtlasByService
var ContainerName = compose.ContainerName
