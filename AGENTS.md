## Core principles

- When you see changes made outside your knowledge, use the current version as your new starting point. Do not blindly overwrite those changes or you sucks. Even if you have to update the code, please god damn respect the pattern, order, whatever!

## Build instructions

- When you need to use `go build` to test builds, make sure the binary is located under `.bin` to be ignored by `.gitignore`. Always run `golangci-lint run ./...` afterwards and add any new linter errors.

## Coding styles

- Use the same coding style as the rest of the project. Pay special attention how line breaks are made for arguments.
- Do not add comments that repeats what the code is doing, use more descriptive names instead.
