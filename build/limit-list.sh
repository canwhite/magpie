#!/bin/sh
# Print what is left of a gateway key's limits, and when each window next
# counts from zero again.
#
# The CLI prints a limit in full (used, left, the window, the state); this
# reads that text and keeps the two things worth watching: how much of each
# window is left, and when the window resets. The numbers are the CLI's, so
# nothing here is a second opinion about them.
#
# usage: build/limit-list.sh <magpie binary> <key id>
set -eu

magpie=$1
key=$2

output=$("$magpie" gateway-key limit "$key")

printf '%s\n' "$output" | awk '
function num(s) {
  if (match(s, /[0-9]+/)) return substr(s, RSTART, RLENGTH)
  return "?"
}
# thousands separated, so a big number reads at a glance
function commas(s,   n, head, rest, out, first) {
  n = s ""
  if (length(n) <= 3) return n
  head = length(n) % 3
  out = ""
  rest = n
  if (head > 0) {
    out = substr(n, 1, head)
    rest = substr(n, head + 1)
  }
  first = (head > 0 ? 0 : 1)
  while (length(rest) > 3) {
    out = out (first ? "" : ",") substr(rest, 1, 3)
    first = 0
    rest = substr(rest, 4)
  }
  if (rest != "") out = out (first ? "" : ",") rest
  return out
}
function flush(   s) {
  if (win == "") return
  if (left == "") {
    s = sprintf("  %-14s no token cap", win)
  } else {
    s = sprintf("  %-14s %s left of %s tokens", win, commas(left), commas(cap))
  }
  if (reset != "") s = s sprintf("   next update %s", reset)
  print s
}
/^Key/ { sub(/^Key  +/, ""); print; next }
{
  if ($0 ~ /^Limit [0-9]+ /) {
    flush()
    win = $0
    sub(/^Limit [0-9]+  +/, "", win)
    sub(/, all must have something left$/, "", win)
    reset = ""; left = ""; cap = ""
  } else if ($0 ~ /^Window /) {
    s = $0
    sub(/^Window  +/, "", s)
    sub(/ \(local time\)$/, "", s)
    reset = substr(s, length(s) - 15)
  } else if ($0 ~ /^Tokens /) {
    s = $0
    sub(/^Tokens  +/, "", s)
    if (s !~ /no cap/) {
      t = s; sub(/^[0-9]+ used of /, "", t)
      cap = num(t)
      if (match(s, /, [0-9]+ left/)) left = num(substr(s, RSTART + 2, RLENGTH - 2))
    }
  }
}
END { flush() }
'
