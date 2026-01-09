# Update Dependencies Command

Check and update all Go module dependencies to their latest versions.

## Check for Updates

1. **List available updates**: Run `go list -m -u all 2>/dev/null | grep '\['` to show all dependencies with updates available.

2. **Display summary**: Show the user which direct and indirect dependencies have updates.

## Update Direct Dependencies

1. **Update all direct dependencies**: Run the following command to update all direct dependencies to their latest versions:
   ```bash
   go get -u github.com/Masterminds/semver/v3 github.com/creativeprojects/go-selfupdate github.com/emersion/go-autostart github.com/fsnotify/fsnotify github.com/getlantern/systray gopkg.in/yaml.v3 tailscale.com
   ```

2. **Tidy the module**: Run `go mod tidy` to clean up the go.mod and go.sum files.

## Verify Updates

1. **Run tests**: Execute `go test ./...` to ensure all tests pass with the updated dependencies.

2. **Run vet**: Execute `go vet ./...` to check for any issues.

3. **Build check**: Optionally run `go build .` to verify the build succeeds.

## Report Results

1. **Show updated packages**: Display which packages were updated and their new versions.

2. **Show test results**: Confirm all tests passed or report any failures.

3. **Remind about GitHub Actions**: Note that GitHub Actions versions should be checked separately via Dependabot PRs or manually in `.github/workflows/`.

## Example Output

```
Checking for dependency updates...

Direct dependencies:
  - tailscale.com v1.92.4 → v1.92.5 (update available)
  - All other direct dependencies are up to date

Updating dependencies...
  go: upgraded tailscale.com v1.92.4 => v1.92.5
  go: upgraded golang.org/x/sys v0.39.0 => v0.40.0
  (plus transitive dependency updates)

Running go mod tidy...
Running tests...
  ok  neubibackup  0.352s
  ok  neubibackup/internal/app  1.184s
  ... (all packages pass)

Running go vet...
  No issues found

Summary:
  - Updated 1 direct dependency
  - Updated 17 indirect dependencies
  - All tests pass
  - No vet issues

Note: GitHub Actions are managed via Dependabot. Check for pending PRs.
```
