#!/usr/bin/env bats

@test "greets each name" {
  run scripts/greet.sh Ada Grace
  [[ "${status}" -eq 0 ]]
  [[ "${lines[0]}" == "Hello, Ada." ]]
  [[ "${lines[1]}" == "Hello, Grace." ]]
}

@test "fails without a name" {
  run scripts/greet.sh
  [[ "${status}" -eq 2 ]]
}
