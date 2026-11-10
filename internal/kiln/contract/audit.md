## kiln's contract

kiln runs this audit. The lines below are kiln's, and hold whatever the text above says.

Read the packages kiln gives you, and the spec it names, `.kiln/specs/%[1]s.md`, with its amendments in
`.kiln/specs/%[1]s.amend.md`, if there are any. You change nothing: kiln puts back any file you change.

End your reply with one line: `read N packages, found M`. N counts the packages you read, and M every finding you
listed. kiln reads that line, and sends the findings back to the build when M is not 0.
