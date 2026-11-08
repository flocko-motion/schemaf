# runtime.sh — container runtime selection for the e2e scripts (sourced).
# Mirrors schemaf: SCHEMAF_RUNTIME selects podman (the default) or docker, any
# other value is an error, and every container call goes through `rt`.
RUNTIME="${SCHEMAF_RUNTIME:-podman}"
case "$RUNTIME" in
  podman|docker) ;;
  *) echo "SCHEMAF_RUNTIME=\"$RUNTIME\" is not a valid container runtime — valid values: podman (the default), docker" >&2; exit 1 ;;
esac
command -v "$RUNTIME" >/dev/null || {
  if [ "$RUNTIME" = podman ]; then
    echo "podman is required (or set SCHEMAF_RUNTIME=docker)" >&2
  else
    echo "SCHEMAF_RUNTIME=docker, but docker is not installed" >&2
  fi
  exit 1
}
export PODMAN_COMPOSE_WARNING_LOGS="${PODMAN_COMPOSE_WARNING_LOGS:-false}"
rt() { "$RUNTIME" "$@"; }
