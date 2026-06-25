# names.sh — emit a random but legible project name by gluing two fantasy words,
# e.g. "amberotter", "mossyfalcon". Lowercase letters only, so the name is safe
# everywhere it's used (Postgres db name, docker compose project, Go module path).
# Sourced by build-example.sh.

_ADJ=(amber azure brisk cedar coral dusky ember fern frost golden hazel ivory
      jade lunar mossy nimbus opal pearl quartz russet sable teal umber velvet willow)
_NOUN=(otter lantern meadow harbor falcon willow ember pebble badger comet cove
       dawn fjord grove heron isle kestrel lark marsh newt oak quill reef sprout)

# random_name prints a glued adjective+noun pair.
random_name() {
  local a=${_ADJ[RANDOM % ${#_ADJ[@]}]}
  local n=${_NOUN[RANDOM % ${#_NOUN[@]}]}
  printf '%s%s' "$a" "$n"
}
