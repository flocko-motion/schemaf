# ATLAS BASE

This is atlas-base - a fundament to quickly build full stack applications 
with minimal effort. Atlas-base provides building blocks for:
- gateway (nginx reverse proxy as gateway to all services - routing /api to backend, / to frontend)
- backend (full golang server with postgres database, api, ai, etc)
- database (postgres)
- frontend (minimal codegen for api.ts to build a web app on top of)
- docker-compose.yml is code-generated from small compose files in compose directory


## Port Convention

```
# Exposed ports
7000    — atlas-base nginx gateway (main entry point for any atlas-base project)
7001    — backend API
7002    — frontend dev server (Vite)
7003    — Postgres
7004 - 7009    — atlas-base reserved
701X    — project services
```
