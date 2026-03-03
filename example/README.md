This is an example project using atlas-base. It shows how to use atlas-base to build a full stack application 
with minimal effort. Atlas-base provides building blocks for:
- gateway (nginx reverse proxy as gateway to all services - routing /api to backend, / to frontend)
- backend (full golang server with postgres database, api, ai, etc)
- database (postgres)
- frontend (minimal codegen for api.ts to build a web app on top of)
- docker-compose.yml is code-generated from small compose files in compose directory
