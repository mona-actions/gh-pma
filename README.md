# gh-pma
Post-Migration Audit (PMA) Extension For GitHub CLI. Used to compare GitHub Enterprise (Server or Cloud) to GitHub Enterprise Cloud (includes Managed Users) migrations.

[![build](https://github.com/mona-actions/gh-pma/actions/workflows/build.yaml/badge.svg)](https://github.com/mona-actions/gh-pma/actions/workflows/build.yaml)
[![release](https://github.com/mona-actions/gh-pma/actions/workflows/release.yaml/badge.svg)](https://github.com/mona-actions/gh-pma/actions/workflows/release.yaml)

## Under Construction
This is a work-in-progress. Currently the tool will report:

- If the repository exists in the target org
- Visibility of repository in target org
- Counts of the following in the source org
  - Secrets
  - Variables
  - Environments

Additionally, you will be prompted to PATCH visibilities if they do not match from source to target.

## Prerequisites
- [GitHub CLI](https://cli.github.com/manual/installation) installed.

## Permissions Required
You will only be able to see repos your account has access too, so make sure your PATs for the source and destination are correct. Additionally, you will need to have full repo permissions if you want to PATCH the visibility from source to target.

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
      --confirm                    Auto respond to confirmation prompt
      --ghes-api-url string        Required if migrating from GHES. The domain name for your GHES instance. For example: ghes.contoso.com (default "github.com")
      --github-source-org string   Uses GH_SOURCE_PAT env variable or --github-source-pat option. Will fall back to GH_PAT or --github-target-pat if not set.
      --github-source-pat string   
      --github-target-org string   Uses GH_PAT env variable or --github-target-pat option.
      --github-target-pat string   
  -h, --help                       help for gh
  -t, --threads int                Number of threads to process concurrently. Maximum of 10 allowed. Increasing this number could get your PAT blocked due to API limiting. (default 3)
  -v, --version                    version for gh
```