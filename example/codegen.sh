#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")"

go run schemaf.local/base/cmd/schemaf codegen migrations
go run schemaf.local/base/cmd/schemaf codegen sqlc
go run schemaf.local/base/cmd/schemaf codegen endpoints
npx --yes swagger-typescript-api generate \
  -p openapi.json \
  -o frontend/src/api/generated \
  --name api.gen.ts
