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

# The runtime that is not selected, for checks that need both (volumes.sh).
# It may be missing: callers check rt_other_usable first.
if [ "$RUNTIME" = podman ]; then OTHER_RUNTIME=docker; else OTHER_RUNTIME=podman; fi
rt_other() { "$OTHER_RUNTIME" "$@"; }
rt_other_usable() { command -v "$OTHER_RUNTIME" >/dev/null && rt_other info >/dev/null 2>&1; }
