#!/bin/sh
# Writes internal/claude/testdata/hand-run.jsonl, the trimmed copy of the hand run's stream that TestHandRun totals.
# It drops each event's text and thinking, and keeps the first line of each tool input and of each result's output.
# Run it from the top of the main checkout, where .hand-run/ holds the stream.
set -eu
mkdir -p internal/claude/testdata
jq -c '
def lines(n): if type == "string" then (split("\n")[0:n] | join("\n")) else . end;
def input: if type == "object" then with_entries(.value |= (if type == "string" then lines(1) else null end)) else . end;
if .type == "assistant" then .message.content |= map(
    if .type == "text" then .text = ""
    elif .type == "thinking" then .thinking = "" | .signature = ""
    elif .type == "tool_use" then .input |= input
    else . end)
elif .type == "user" then del(.tool_use_result) | .message.content |= (if type == "string" then "" else map(
    if .type == "tool_result" then .content |= (if type == "string" then lines(5)
        elif type == "array" then map(if .type == "text" then .text |= lines(5) else {type} end)
        else . end)
    elif .type == "text" then .text = ""
    else . end) end)
elif .type == "result" then .result = "" | .permission_denials |= map(.tool_input |= input)
else . end' .hand-run/stream.jsonl >internal/claude/testdata/hand-run.jsonl
