# gh-pma
Post-Migration Audit (PMA) Extension For GitHub CLI. Used to compare GitHub Enterprise (Server or Cloud) to GitHub Enterprise Cloud (includes Managed Users) migrations.

[![build](https://github.com/mona-actions/gh-pma/actions/workflows/build.yaml/badge.svg)](https://github.com/mona-actions/gh-pma/actions/workflows/build.yaml)
[![release](https://github.com/mona-actions/gh-pma/actions/workflows/release.yaml/badge.svg)](https://github.com/mona-actions/gh-pma/actions/workflows/release.yaml)

## Under Construction
This is a work-in-progress. Currently the tool will report:

- Repositories migrated and if any of the following need to be manually copied:
  - Secrets
  - Variables
  - Environments

## Prerequisites
- [GitHub CLI](https://cli.github.com/manual/installation) installed.

## Permissions Required
Put link to migration permissions here

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
      --ghes-api-url string        Required if migrating from GHES. The domain name for your GHES instance. For example: ghes.contoso.com (default "github.com")
      --github-source-org string   Uses GH_SOURCE_PAT env variable or --github-source-pat option. Will fall back to GH_PAT or --github-target-pat if not set.
      --github-source-pat string   
      --github-target-org string   Uses GH_PAT env variable or --github-target-pat option.
      --github-target-pat string   
  -h, --help                       help for gh
      --no-ssl-verify              Only effective if migrating from GHES. Disables SSL verification when communicating with your GHES instance. All other migration steps will continue to verify SSL. If your GHES instance has a self-signed SSL certificate then setting this flag will allow data to be extracted.
      --output-file string         The file to output the results to. (default "results.csv")
  -t, --threads int                Number of threads to process concurrently. Maximum of 10 allowed. Increasing this number could get your PAT blocked due to API limiting. (default 3)
  -v, --version                    version for gh
```