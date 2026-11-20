## kiln's contract

kiln runs this audit. The lines below are kiln's, and hold whatever the text above says.

Read the directories kiln gives you. They hold the fix a monitor's agent made, the monitor `%[1]s` in
`.kiln/monitors/%[1]s/`. A fix has no spec: judge it by whether it fixes what the monitor looks for, and breaks
nothing else. A directory means its own files, not its subdirectories. You change nothing: kiln puts back any file you
change.

End your reply with one line: `read N directories, found M`. N counts the directories you read, and M every finding
you listed. kiln reads that line, and sends the findings back to the fix's session when M is not 0.
