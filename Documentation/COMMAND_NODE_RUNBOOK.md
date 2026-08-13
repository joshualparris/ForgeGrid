# ForgeGrid Command Node Runbook

This runbook is for validating ForgeGrid on real trusted laptops before using it for day-to-day app and game development.

## Worker GitHub Credentials

Prefer a separate SSH deploy key or machine user per worker. Give each worker access only to the repositories it should touch.

Recommended setup on each worker:

```bash
ssh-keygen -t ed25519 -f ~/.ssh/forgegrid_ed25519 -C "forgegrid-worker"
```

Add the public key to GitHub for the allowed repository or machine user. Then configure SSH:

```sshconfig
Host github.com
  IdentityFile ~/.ssh/forgegrid_ed25519
  IdentitiesOnly yes
```

Do not put GitHub tokens in manifests, dashboard text, environment variables shown in logs, or ForgeGrid config files. If HTTPS tokens are required, use the OS credential manager or GitHub CLI auth on the worker and verify logs do not print credentials.

For PR creation, install and authenticate GitHub CLI on the worker:

```bash
gh auth login
gh auth status
```

## Physical LAN Test

1. Start the coordinator on the command node:

```bash
./forgegrid -mode coordinator -port 8080
```

2. Open the dashboard URL printed by the coordinator and generate a pairing code.

3. Start each worker with explicit repository and capability policy:

```bash
./forgegrid -mode worker \
  -name "linux-build-1" \
  -coordinator 192.168.1.10 \
  -code 123456 \
  -fingerprint <coordinator-fingerprint> \
  -allowed-repos "git@github.com:you/game.git" \
  -labels "trusted,linux-build" \
  -capabilities "go,node,codex,godot,github-pr"
```

Only add `-allow-push` on workers that are trusted to push:

```bash
./forgegrid -mode worker ... -allow-push
```

Alternatively, write a persistent runner policy on each runner once:

```bash
./forgegrid -mode worker -write-worker-policy \
  -allowed-repos "git@github.com:you/game.git" \
  -labels "trusted,linux-build" \
  -capabilities "go,node,codex,godot,github-pr"
```

The dashboard's Runner Policy Setup card generates this command.

4. Confirm each worker appears online with the expected labels and capabilities.

5. Dispatch a no-push job first. Confirm it runs, logs appear, and no Git branch is pushed.

6. Dispatch a commit-only job. Confirm the worker retains the worktree path in the logs and does not push.

7. Dispatch a push job only after verifying:

- the repo URL is allowlisted on that worker
- the branch name is unique, e.g. `forgegrid/<project>-<task>-<date>`
- the worker was launched with `-allow-push`
- GitHub credentials on that worker are scoped correctly

8. For PR tests, set `repository.create_pr: true` and ensure `gh auth status` passes on the worker.

9. For artifact tests, declare `artefacts: ["build/**"]`. Small files should appear as dashboard download links. Larger compressible files are uploaded as `.zip` packages when the compressed result fits the upload cap.

## Firewall Checks

If workers cannot pair:

- Confirm the coordinator and workers are on the same LAN.
- Allow inbound TCP traffic to the coordinator port, usually `8080`.
- Use the LAN IP printed by the coordinator, not `127.0.0.1`.
- Confirm the TLS fingerprint copied to the worker exactly matches the coordinator output.

## Recovery And Rollback

If a worker goes offline during a job, ForgeGrid marks the attempt failed and creates a retry when `execution.max_retries` permits it.

If a push is wrong:

```bash
git push origin --delete <branch>
```

If a PR was created, close it in GitHub or with:

```bash
gh pr close <url-or-number>
```

If worker credentials are stale:

```bash
./forgegrid -mode worker -reset-worker
```

## Security Checklist

- Keep repo allowlists narrow.
- Keep `-allow-push` off by default.
- Use unique branches for every pushed job.
- Use `base_commit` as a pinned commit SHA, not a moving branch name, for important work.
- Do not expose GitHub tokens in manifests, logs, docs, dashboard text, or environment dumps.
- Use labels/capabilities to keep AI-agent jobs on trusted workers only.
- Review diffs and PRs before merging.
