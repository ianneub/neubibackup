# Release Command

Create a new release for NeubiBackup following semantic versioning.

## Pre-flight Checks

1. **Verify clean working directory**: Run `git status` and confirm there are no uncommitted changes. If there are changes, stop and inform the user.

2. **Fetch latest from origin**: Run `git fetch origin` to ensure we have the latest remote state.

3. **Verify branch is up to date with main**:
   - Check that the current branch is `main`
   - Run `git rev-parse HEAD` and `git rev-parse origin/main` to compare local and remote
   - If they differ, stop and inform the user that local is not in sync with origin/main

## Determine Version Number

1. **Get existing tags from origin**: Run `git tag -l 'v*' --sort=-v:refname` to list all version tags sorted by version (newest first).

2. **Parse the latest version**: Extract the most recent version tag (e.g., `v1.2.0`).

3. **Analyze commits since last tag**: Run `git log <last-tag>..HEAD --oneline` to see what changed.

4. **Determine version bump** based on commit messages following semantic versioning:
   - **MAJOR** bump (breaking changes): Look for commits with `BREAKING CHANGE:` or `!:` in the message
   - **MINOR** bump (new features): Look for commits starting with `feat:` or `feature:`
   - **PATCH** bump (bug fixes): Look for commits starting with `fix:`, `docs:`, `chore:`, `refactor:`, `test:`, `style:`, `perf:`, or `ci:`
   - Default to PATCH if unclear

5. **Calculate new version**: Increment the appropriate version component and reset lower components to 0.

## Create and Push Tag

1. **Create the tag**: Run `git tag -a v<new-version> -m "Release v<new-version>"`

2. **Push the tag to origin**: Run `git push origin v<new-version>`

3. **Inform the user**: Display the new version number and that the tag has been pushed.

## Wait for Release Workflow

1. **Find the workflow run**: Use `gh run list --workflow=release.yml --limit=1 --json databaseId -q '.[0].databaseId'` to get the run ID.

2. **Wait for completion**: **ALWAYS** use `gh run watch <run-id>` to wait for the workflow to complete. This command will block until the workflow finishes and display real-time progress. Do NOT poll manually or use other methods.

3. **Verify success**: After `gh run watch` completes, confirm the workflow succeeded. If it failed, inform the user and provide the workflow URL.

## Generate Release Notes

1. **Get commits since last version**: Run `git log <previous-tag>..v<new-version> --pretty=format:"- %s"` to get a list of changes.

2. **Generate a concise description**: Create a short, human-readable summary of the changes. Group by type if there are multiple commits:
   - Features (feat:)
   - Bug Fixes (fix:)
   - Other changes

3. **Update the GitHub release**: Use `gh release edit v<new-version> --notes "<generated-notes>"` to add the release description.

4. **Provide the release URL**: Display `https://github.com/ianneub/neubibackup/releases/tag/v<new-version>` for the user.

## Example Output

```
Pre-flight checks passed:
  - Working directory is clean
  - Local main is in sync with origin/main

Current version: v1.2.0
Commits since last release:
  - feat: implement macOS Full Disk Access handling

Version bump: MINOR (new feature detected)
New version: v1.3.0

Creating tag v1.3.0...
Pushing tag to origin...

Waiting for release workflow...
  Run #123: in_progress
  Run #123: completed (success)

Updating release notes...

Release v1.3.0 published!
https://github.com/ianneub/neubibackup/releases/tag/v1.3.0

## What's Changed
- Implement macOS Full Disk Access handling and related tests
```
