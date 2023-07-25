# gh-pma
Post-Migration Audit (PMA) Extension For GitHub CLI. Used to compare GitHub Enterprise (Server or Cloud) to GitHub Enterprise Cloud (includes Managed Users) migrations after using the [GEI](https://github.com/github/gh-gei) tool.

Will report on:
- Repositories from source exist in target
- Repository target visibilities match source
- If the following exist in source repositories:
   - Secrets
   - Variables
   - Environments

The tool could be expanded to include other non-migratable settings (see [what is & isn't migrated](https://docs.github.com/en/migrations/using-github-enterprise-importer/understanding-github-enterprise-importer/migration-support-for-github-enterprise-importer#githubcom-migration-support) during a migration with [GEI](https://github.com/github/gh-gei)), and most likely will in the near future.

[![build](https://github.com/mona-actions/gh-pma/actions/workflows/build.yaml/badge.svg)](https://github.com/mona-actions/gh-pma/actions/workflows/build.yaml)
[![release](https://github.com/mona-actions/gh-pma/actions/workflows/release.yaml/badge.svg)](https://github.com/mona-actions/gh-pma/actions/workflows/release.yaml)

## Planned Updates
- Add detection of:
   - Codespaces Secrets
   - Dependabot Secrets
   - Environmen Secrets & Vars (currently just Environments are detected)
- Create an issue on the target org repository with the list of items to mitigate.

## Prerequisites
- [GitHub CLI](https://cli.github.com/manual/installation) installed.

## Permissions Required
You will only be able to see repos your account has access too, so make sure your PATs for the source and destination are correct. Just like [GEI](https://github.com/github/gh-gei), the tool uses `GH_PAT` and `GH_SOURCE_PAT` environment variables to communicate with source and target, unless you manually override with the provided flags.

Those PATs should adhere to the same scopes as [GEI](https://github.com/github/gh-gei) (see [documentation](https://docs.github.com/en/migrations/using-github-enterprise-importer/preparing-to-migrate-with-github-enterprise-importer/managing-access-for-github-enterprise-importer#required-scopes-for-personal-access-tokens)).

## Install

```bash
$ gh extension install mona-actions/gh-pma
```

## Upgrade
```bash
$ gh extension upgrade pma
```

## Usage

```txt
$ gh pma [flags]
```

```txt
Post-Migration Audit (PMA) Extension For GitHub CLI. Used to compare GitHub Enterprise (Server or Cloud) to GitHub Enterprise Cloud (includes Managed Users) migrations.

Usage:
  gh pma [flags]

Flags:
      --confirm                    Auto respond to visibility alignment confirmation prompt
  -c, --create-csv                 Whether to create a CSV file with the results.
  -i, --create-issues              Whether to create issues in target org repositories or not.
      --ghes-api-url string        Required if migrating from GHES. The domain name for your GHES instance. For example: ghes.contoso.com (default "github.com")
      --github-source-org string   Uses GH_SOURCE_PAT env variable or --github-source-pat option. Will fall back to GH_PAT or --github-target-pat if not set.
      --github-source-pat string   
      --github-target-org string   Uses GH_PAT env variable or --github-target-pat option.
      --github-target-pat string   
  -h, --help                       help for gh
      --threads int                Number of threads to process concurrently. Maximum of 10 allowed. Increasing this number could get your PAT blocked due to API limiting. (default 3)
  -v, --version                    version for gh
```