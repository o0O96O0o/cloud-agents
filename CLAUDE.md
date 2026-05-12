# Rules

- *ALWAYS* actively ask questions if there is anything unclear.
- *ALWAYS* update related documents if you made functional changes.
- Add under a ## Planning & Design section near the top of CLAUDE.md\n\nWhen asked to plan or design something, create structured plan/spec artifacts (e.g., in /plans/ or via OpenSpec workflow) BEFORE writing any implementation files. Only proceed to code after plan is reviewed.
- Add under ## Testing section\n\nAlways run the full test suite with race detection (e.g., `go test -race ./...`) after multi-file changes, and verify clean build before reporting completion.
- Add under ## Code Modification Guidelines\n\nBefore removing existing logic (waits, retries, deduplication, lifecycle hooks), explain why it exists and confirm with the user. Do not assume code is dead.
