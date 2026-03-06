#!/bin/bash
cd "$(dirname "$0")"
go run github.com/yourorg/schemaf/cmd/schemaf codegen .
