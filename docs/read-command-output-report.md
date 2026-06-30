# CLI read command output report

Date: 2026-06-30

Scope:

- CLI repository: `/Users/chingis/Projects/wodby-cli`
- Dashboard repository: `/Users/chingis/Projects/dashboard`
- Backend repository: `/Users/chingis/Projects/backend`

This report compares the information shown by CLI read/list/get commands against the dashboard views that solve the same user problems. The dashboard is generally a good signal because its list and detail pages already encode which fields operators need first. The CLI uses REST while the dashboard uses GraphQL, so each recommendation marks whether the CLI can change now or whether REST should expose more data first.

## Executive summary

The CLI already has strong infrastructure for table, vertical, and JSON output, plus relation enrichment and several derived status commands. The main gap is not rendering capability. The gap is information design: many default tables show raw IDs, machine booleans, or creation timestamps where the dashboard shows operational state, last activity, revision/version context, source, ownership, and compact health badges.

The biggest changes I recommend:

1. Make operational list commands activity-first.
   Use `startedAt`, `endedAt`, and `duration` for builds, deployments, backups, imports, and tasks. Keep `createdAt` in get/vertical or `--wide`.

2. Add compact `state` columns for app instances and app services.
   The dashboard does not rely on raw `status` alone. It overlays configuration required, needs rebuild, needs redeploy, build/deploy failure, outdated, and EOL indicators. CLI users need the same summary, even if text-only.

3. Prefer user-facing summaries over raw internals in default tables.
   Examples: `revision` instead of `revId`, `route` instead of separate `host`/`path`, `source` instead of several nullable relation IDs, `location` instead of separate `region`/`zone` when space is tight.

4. Add REST summary fields where GraphQL currently has the dashboard-only view.
   The CLI should not reproduce the dashboard by issuing many extra REST requests per row. REST list DTOs should expose display summaries for list pages.

5. Keep default tables compact and move diagnostic detail into get/vertical/JSON.
   Some current defaults, especially routes and builds, include too many low-priority columns. The dashboard generally chooses 7-9 meaningful columns and hides the rest behind detail views.

6. Show pagination context.
   The CLI unwraps list responses for table output but drops metadata like totals and next page. Add a short footer such as `Showing 20 of 144. Next page: 2` when REST includes that data.

## Current CLI behavior

The CLI read commands are declared in `cmd/wodby/ops/commands.go`, with table rendering and relation enrichment in `cmd/wodby/ops/helpers.go`.

Strengths:

- `--output table|vertical|json` is already available.
- Get operations render vertical tables, which is appropriate for detail output.
- Relation columns are enriched with related resources where possible.
- Time values are already formatted as relative time in tables and absolute time in vertical output.
- `app status` and `instance status` already compute useful aggregate state from services, routes, ports, builds, and deployments.

Weaknesses:

- Many default list columns are schema-first rather than workflow-first.
- Operational histories often show `createdAt` instead of `startedAt`, `endedAt`, and `duration`.
- Detail commands still omit fields that REST already exposes, especially task, import, port, project, env, member, stack, and deployment fields.
- Several commands show raw revision IDs instead of revision numbers and versions.
- Some columns duplicate information or are too narrow to be useful in default output, such as many route certificate fields in the default route list.
- Dashboard-only GraphQL fields are often exactly the fields that make list views useful: latest build/deploy, stack revision, source, location, high availability, image status, EOL, storage, and ownership.

## Output principles

Use these rules when changing command output:

1. Default table output should answer "what needs attention?" and "what changed recently?"
2. Get/vertical output should answer "what is this object exactly?"
3. JSON output should remain the complete API payload.
4. IDs are useful but should not crowd out titles, names, numbers, and statuses.
5. Boolean columns should usually become state labels:
   - `disabled=false` becomes `enabled`
   - `needsRebuild=true` becomes part of `state`
   - `singleNode=true` becomes part of `nodes`
6. Use the same mental model as the dashboard:
   - `state` for status plus actionable flags
   - `source` for origin/integration/application
   - `revision` for revision number/version, not `revId`
   - `location` for country/region/zone
   - `started` and `duration` for job-like resources

## Cross-cutting CLI recommendations

### Add a compact state formatter

Add a reusable text formatter for state columns:

- App instance state: `Ready`, `Deploying`, `Build failed`, `Deploy failed`, `Config required`, `Needs rebuild`, `Needs redeploy`, `Outdated`, `EOL`
- App service state: `Ready`, `Config required`, `Needs rebuild`, `Needs redeploy`, `EOL`
- Cluster state: `Ready`, `Outdated`, `Monitoring off`, `Awaiting install`
- Route/cert state: `Ready`, `Syncing`, `Disabled`, `Cert expired`, `Cert pending`

The current CLI has raw fields for some of this, but dashboard state components combine them into the primary scanning column. For table output, use comma-separated tags or short labels. For JSON, preserve raw fields.

### Add `--wide` or `--columns`

Default output should be compact. Some operators still need raw fields, so add either:

- `--wide`, with an expanded but curated column set.
- `--columns`, for explicit custom columns.

This would let defaults align with the dashboard without removing power-user access.

### Show pagination metadata

The table renderer normalizes wrapped list responses and then discards response metadata. When the API returns total, page, page size, or next page, table output should print a short footer. This matters more after defaults become dashboard-like because users need to know whether they are seeing a full list.

### Standardize time columns

Use these conventions:

- `created`: administrative resources and catalog resources.
- `updated`: config resources where recency means last change.
- `started`: tasks, builds, deployments, backups, and imports.
- `duration`: anything with start/end timestamps.
- `deployed`: app instances and services where the latest successful deployment is the key event.

### Standardize revision display

The dashboard consistently uses revision numbers and versions. CLI defaults should avoid raw `revId` where possible.

Recommended display:

- `revision`: `#12 1.4.3`
- `stack`: `Drupal #17`
- `service`: `nginx #9 1.25`
- Keep `revId` in get/vertical/JSON.

### Embed summary relations in REST list DTOs

The CLI currently enriches relation IDs by fetching related resources. This works, but dashboard-equivalent output would require too many requests for app instances, builds, deployments, databases, backups, and integrations.

REST should expose small summary objects or flattened display fields in list DTOs:

```json
{
  "app": { "id": "...", "name": "...", "title": "..." },
  "cluster": { "id": "...", "name": "...", "title": "...", "icon": "..." },
  "stackRev": { "id": "...", "number": 17, "version": "1.2.0" }
}
```

Flattened fields are also acceptable for CLI use, but summary objects are more extensible.

## Resource-by-resource recommendations

### Apps and app instances

Dashboard behavior:

- The dashboard app list is app-instance-centric.
- It shows app, instance, metrics, stack, cluster, domain, state, and deployed time.
- State includes raw status plus build failure, deploy failure, building, configuration required, needs rebuild, needs redeploy, outdated, and EOL.
- Detail views show owner, environment, stack revision, cluster, CI/registry integrations, created/updated, and last deployed.

Current CLI:

- `app list`: `id`, `name`, `title`, `status`, `stack`, `clusterApp`.
- `app get`: adds `instances`, `createdAt`, `updatedAt`.
- `instance list`: `id`, `name`, `title`, `status`, `outdated`, `app`, `stack`, `env`, `cluster`, `domain`.
- `instance get`: adds `serviceStatus`, `routeStatus`, `portStatus`, `createdAt`, `updatedAt`.
- `app status` and `instance status` compute richer aggregate health.

Recommended CLI defaults:

- `app list`: `id`, `title`, `name`, `status`, `instances`, `state`, `updated`.
- `instance list`: `id`, `app`, `instance`, `env`, `stack`, `cluster`, `domain`, `state`, `deployed`.
- `instance get`: keep existing detail and add latest build/deploy, owner, CI integration, registry integration, created/updated, and configuration issues.
- Consider making `instance list` the primary documented command for the dashboard-equivalent app view.

CLI can improve now:

- Reuse the existing `app status` and `instance status` aggregation logic for a new `state` formatter.
- Display app instance `mainDomain` as `domain`.
- Use `stackRevNumber` and `stackVersion` already present in REST `AppInstance`.

REST gaps:

- `AppInstance` REST lacks `building`, `needsRebuild`, `needsRedeploy`, `configurationReady`, `configurationIssues`, `outdated`, latest build summary, latest deploy summary, last successful deployment, CI integration, registry integration, owner/ownership, and service EOL rollup.
- Dashboard GraphQL exposes most of this through app instance, app service, build, deploy, cluster, and stack revision fields.

Priority: High. This is the main read surface users will compare to the dashboard.

### App services

Dashboard behavior:

- List columns: name, version, enabled, status, replicas, storage GB, last deploy.
- State includes configuration required, needs rebuild, and needs redeploy.
- Detail shows machine name, type, version and EOL, service revision/version, database, images, build image, last built, last deploy, and configuration issues.

Current CLI:

- `service list` under an app instance: `id`, `name`, `title`, `type`, `status`, `version`, `replicas`, `disabled`, `main`, `needsRebuild`, `needsRedeploy`, `configurationReady`.

Recommended CLI defaults:

- `app-service list`: `id`, `service`, `version`, `enabled`, `state`, `replicas`, `storage`, `lastDeploy`.
- `app-service get`: include service revision, EOL, images, build image, last built, last deploy, database relation, config issues, and required/external/main flags.

CLI can improve now:

- Replace `disabled` with `enabled`.
- Fold `needsRebuild`, `needsRedeploy`, and `configurationReady` into `state`.
- Keep raw booleans in JSON and vertical output.

REST gaps:

- `AppService` REST lacks `eol`, deployment summary, last successful build, storage usage, config issue details, image/build image summaries, database relation summary, and service revision number/version beyond `serviceRevId`.

Priority: High.

### Routes, ports, and certificates

Dashboard behavior:

- Routes list shows service, route, action, certificate issuer, primary, private, status, and updated time.
- Route display combines host and path.
- Ports list shows service, name, port, public port, private, protocol, and updated time.
- Certificates list shows issuer, key type, key length, used, status, issued, expires, and renews.

Current CLI:

- `route list`: `id`, `host`, `path`, `pathType`, `action`, `status`, `service`, `port`, `cert`, `certStatus`, `certIssuer`, `certExpiresAt`, `main`, `primary`, `private`, `disabled`, `lastSyncedAt`, `createdAt`.
- `port list`: `id`, `name`, `number`, `protocol`, `private`, `service`, `instance`, `createdAt`.
- `cert list`: `id`, `host`, `status`, `issuer`, `certType`, `expiresAt`, `route`, `instance`, `createdAt`.

Recommended CLI defaults:

- `route list`: `id`, `service`, `route`, `action`, `cert`, `primary`, `private`, `state`, `updated`.
- `route get`: include host, path, path type, redirect fields, cert status/issuer/expires, disabled, main, primary, private, last synced, created/updated.
- `port list`: `id`, `service`, `name`, `port`, `publicPort`, `private`, `protocol`, `updated`.
- `cert list`: `id`, `host`, `issuer`, `key`, `used`, `status`, `issued`, `expires`, `renews`.

CLI can improve now:

- REST `AppRoute` already includes redirect fields and `updatedAt`; add them to get/vertical.
- REST `AppPort` already includes `publicPort` and `updatedAt`; use them.
- REST `Cert` includes key type, key length, issued, renews, expires; use them when the command is backed by `/certs`.

REST gaps:

- The app certificate endpoint used by the CLI may not expose the same fields as org-level `Cert`. Align app cert DTOs with dashboard certificate fields.
- A `used` count/boolean for certificates should be added if the CLI cannot infer it cheaply.

Priority: Medium-high. The current route table is too wide and still misses the most useful formatted route/updated view.

### Builds

Dashboard behavior:

- List columns: number, app service, images, repository/ref, stack revision, started, duration, status.
- Detail shows image void status, stack revision, deployments, CI integration, logs, repository, workflow, pipeline, commit message/hash/author, started/ended/duration, and per-service built images with size/status/deleted.

Current CLI:

- `build list`: `id`, `number`, `status`, `instance`, `service`, `services`, `task`, `gitRefType`, `gitRef`, `commitHash`, `commitMessage`, `createdAt`, `startedAt`, `endedAt`, `duration`.
- `build get`: adds `images`.

Recommended CLI defaults:

- `build list`: `id`, `number`, `service`, `images`, `repo`, `ref`, `stackRev`, `started`, `duration`, `status`.
- `build get`: include instance, task, image void status, CI integration, workflow, pipeline, commit author, commit hash/message, deployments, started/ended/duration, and image rows with image, size, status, deleted.

CLI can improve now:

- Use `startedAt` instead of `createdAt` in default output.
- Display an image count for list rows from `appServiceBuilds`.
- Display per-service image details in get output from REST `AppServiceBuild`.

REST gaps:

- `AppBuild` REST lacks `imageVoidStatus`, `canVoidImages`, Git repository summary, CI integration summary, stack revision summary, workflow, pipeline, commit author fields, and deployment summary.

Priority: High. Build output is one of the clearest dashboard-vs-CLI mismatches.

### Deployments

Dashboard behavior:

- List columns: number, deployed app services count, builds count, post-deploy scripts status, stack revision, started, duration, status.
- Detail shows post-deploy script status, stack revision, builds, task logs, started/ended/duration, and per-service deployment rows with build number, skip post-deploy, force, logs, duration, and status.

Current CLI:

- `deployment list`: `id`, `number`, `status`, `instance`, `services`, `task`, `skipRollback`, `createdAt`, `startedAt`, `endedAt`.
- `deployment get`: adds `images`.

Recommended CLI defaults:

- `deployment list`: `id`, `number`, `services`, `builds`, `postDeploy`, `stackRev`, `started`, `duration`, `status`.
- `deployment get`: include task, logs pointer, skip rollback, stack revision, builds, per-service deployment status, app service build number, skip post-deploy, force, started/ended/duration.

CLI can improve now:

- Add `duration` to list output.
- Use `startedAt` as the primary time column.
- Use existing `AppServiceDeployment` REST fields in get output.

REST gaps:

- `AppDeployment` REST lacks stack revision summary, post-deployment script status/list, and `skipPostDeploymentScripts`.
- It may also need enabled service counts to match the dashboard count format without extra requests.

Priority: High.

### Backups

Dashboard behavior:

- List columns: app service, backup title, integration, bucket, status, started, duration.
- Detail shows ID, status, app service, name/title, logs, storage integration, bucket, overridden storage class, started/ended/duration, and a download link action when completed.

Current CLI:

- `backup list`: `id`, `name`, `status`, `instance`, `service`, `database`, `databaseDb`, `task`, `createdAt`.

Recommended CLI defaults:

- `backup list`: `id`, `target`, `backup`, `integration`, `bucket`, `status`, `started`, `duration`.
- `backup get`: include title, storage integration, bucket, storage class, URL/download availability, task/logs, started/ended/duration, created/updated.

CLI can improve now:

- Limited. REST currently does not expose enough backup timing and storage detail.

REST gaps:

- `Backup` REST lacks `title`, `integrationId` or integration summary, `bucket`, `storageClass`, `url`, `taskId`, `startedAt`, and `endedAt`.

Priority: High because current backup output omits most fields operators use in the dashboard.

### Imports

Dashboard behavior:

- List columns: app service, import title, source, status, started, duration.
- Source is displayed as uploaded file, URL, or backup.
- Detail shows source detail, backup reference, task/logs, started/ended/duration.

Current CLI:

- `import list`: `id`, `name`, `source`, `status`, `task`, `instance`, `service`, `database`, `databaseDb`, `createdAt`.

Recommended CLI defaults:

- `import list`: `id`, `target`, `import`, `source`, `status`, `started`, `duration`.
- `import get`: include source detail, URL or backup, task/logs, job name, started/ended/duration, created/updated.

CLI can improve now:

- REST `Import` already has `taskId`, `backupId`, `startedAt`, and `endedAt`; use `started` and `duration` in the default table.

REST gaps:

- `Import` REST lacks `title`, `url`, and `jobName`.

Priority: Medium-high.

### Tasks

Dashboard behavior:

- List columns: name, status, projects, user, started, duration.
- Task model includes progress, projects, origin/retry, repeated task, spawned tasks, jobs, steps, log status, system flags, started, and ended.

Current CLI:

- `task list`: `id`, `title`, `status`, `author`, `createdAt`, `duration`.
- `task get`: adds app, instance, service, database, and database DB.
- `task jobs` and `task steps` show basic job/step status and duration.

Recommended CLI defaults:

- `task list`: `id`, `title`, `status`, `progress`, `projects`, `author`, `started`, `duration`.
- `task get`: include origin task, repeated task, spawned tasks, started, ended, progress, projects, silent/system flags, and all related object IDs.
- `task jobs`: `id`, `name`, `status`, `logStatus`, `system`, `started`, `duration`, `steps`.
- `task steps`: `id`, `name`, `status`, `logStatus`, `system`, `started`, `duration`, `job`.

CLI can improve now:

- REST `Task`, `TaskJob`, and `TaskStep` already expose most of this.
- Switch default time from `createdAt` to `startedAt`.
- Add `progress`, `projects`, `startedAt`, and `endedAt` columns/fields.

REST gaps:

- Dashboard has pipeline/job links that may not be meaningful in CLI unless REST exposes a stable log URL or pipeline URL. Otherwise no major REST gap for task basics.

Priority: High, because REST already has the data and the CLI output can become more useful quickly.

### Clusters

Dashboard behavior:

- List columns: name, metrics, nodes, integration, location, status, created.
- Detail shows owner, monitoring status, public IPs, Kubernetes version, infra version and outdated state, location, node capacity, provider integration details, node counts, created/updated, and K3S install command if awaiting.

Current CLI:

- `cluster list`: `id`, `name`, `title`, `status`, `integration`, `region`, `zone`, `version`, `nodes`, `singleNode`, `scalable`, `serverless`.
- `cluster get`: adds Kubernetes version, infra version, IPs.

Recommended CLI defaults:

- `cluster list`: `id`, `cluster`, `integration`, `location`, `nodes`, `monitoring`, `state`, `created`.
- `cluster get`: include owner, K8s version, infra version, outdated, public IPs, integration external ID/hostname/machine type, location, node min/max/last, capacity, monitoring, created/updated.

CLI can improve now:

- Combine region/zone into `location`.
- Combine single-node/scalable/serverless/min/max/last into a clearer `nodes` column.
- Include `createdAt` in list and `updatedAt` in get.

REST gaps:

- `Cluster` REST lacks `country`, `disableMonitoring`, `outdated`, external ID, machine type, node disk size, allocatable capacity, overhead capacity, and ownership display fields.

Priority: Medium.

### Databases

Dashboard behavior:

- List columns: instance, kind plus high availability, version, environment, source, location, status, created.
- Detail shows owner, type, environment, kind/version/HA, source, master password action, application source, private host/SSL, external integration, resided cluster, storage size, IOPS, machine type, external ID, public/private hosts and ports, SSL, created/updated.

Current CLI:

- `database list`: `id`, `name`, `title`, `status`, `kind`, `type`, `version`, `env`, `integration`, `service`, `region`, `zone`.

Recommended CLI defaults:

- `database list`: `id`, `database`, `kind`, `version`, `env`, `source`, `location`, `state`, `created`.
- `database get`: include HA/replicas, source, application or integration, host/port/SSL, storage, IOPS, machine type, external ID, resided cluster, owner, created/updated.

CLI can improve now:

- Combine integration/app service into a `source` display.
- Combine region/zone into `location`.
- Add `createdAt` to default list if space allows.

REST gaps:

- `Database` REST lacks `highAvailability`, `replicas`, `country`, `external`, owner/ownership, host/port/SSL fields, public host/port, private host/port, storage size, IOPS, machine type, external ID, resided cluster, app/instance summaries, and integration summaries.

Priority: Medium-high.

### Database DBs and users

Dashboard behavior:

- DB and user tables show names, status, charset/collation or host, granted DBs, and created time.

Current CLI:

- DB columns: `id`, `name`, `status`, `charset`, `collation`, `database`, `createdAt`.
- User columns: `id`, `username`, `hostname`, `status`, `database`, `dbs`, `createdAt`.

Recommended CLI defaults:

- Mostly keep current output.
- Add `updatedAt` to get/vertical output.
- For users, display `user` as `username@hostname` when both are present.

REST gaps:

- No major list-view gap identified.

Priority: Low.

### Integrations

Dashboard behavior:

- List columns: name, provider, scope, types, status, created, updated.
- Types come from provider revision kinds.

Current CLI:

- `integration list`: `id`, `name`, `title`, `scope`, `status`, `provider`, `createdAt`.

Recommended CLI defaults:

- `integration list`: `id`, `integration`, `provider`, `scope`, `types`, `status`, `created`, `updated`.
- `integration get`: include auth mode/status, provider revision, kinds/types, shared projects/owner if applicable.

CLI can improve now:

- Add `updatedAt`.

REST gaps:

- `Integration` REST lacks provider revision summary, provider kinds/types, owner/ownership, and shared project summaries.

Priority: Medium.

### Providers

Dashboard behavior:

- List columns: name, integration types, version/revision, maintainer, created, updated.

Current CLI:

- `provider list`: `id`, `name`, `title`, `status`, `public`, `revId`.

Recommended CLI defaults:

- `provider list`: `id`, `provider`, `types`, `version`, `revision`, `status`, `maintainer`, `updated`.
- `provider get`: include revision ID, revision number/version, manifest kinds, source/maintainer, created/updated.

CLI can improve now:

- If the REST provider get/list can fetch `ProviderRevision`, format revision number/version instead of raw `revId`.
- Add `createdAt` and `updatedAt` to get output.

REST gaps:

- Provider list DTO lacks revision number/version, manifest kinds/types, maintainer/org summary, and source metadata.

Priority: Medium.

### Catalog services

Dashboard behavior:

- List columns: name, type, version, revision number, status, created, updated.

Current CLI:

- Catalog service columns: `id`, `name`, `title`, `type`, `status`, `public`, `external`, `revId`, `latestRevNumber`.

Recommended CLI defaults:

- `catalog service list`: `id`, `service`, `type`, `version`, `revision`, `status`, `created`, `updated`.
- `catalog service get`: include public/external, service revision ID, revision number/version, manifest summary, EOL/build support if available.

CLI can improve now:

- Replace raw `revId` with revision number/version when a revision can be fetched.
- Move `public` and `external` to get/vertical or `--wide`.

REST gaps:

- Service list DTO lacks current revision number/version and manifest-derived display fields.
- It also lacks owner/source metadata and EOL/build-support summaries.

Priority: Medium.

### Stacks

Dashboard behavior:

- List columns show name/icon, draft/source indicators, revision number, status/outdated/EOL, created, and updated.
- Detail includes source Git repo, origin stack revision, owner, current/latest revision, services, env vars, tokens, annotations, and created/updated.

Current CLI:

- `stack list`: `id`, `name`, `title`, `status`, `revision`, `currentVersion`, `outdated`, `createdAt`, `updatedAt`.
- `stack get`: adds `public`, `revId`, `currentRevNumber`, `latestRevNumber`, and `services`.

Recommended CLI defaults:

- `stack list`: `id`, `stack`, `revision`, `version`, `state`, `source`, `created`, `updated`.
- `stack get`: include owner, public, draft revision, latest revision, source Git repo/ref, origin stack revision, services with service revision/version, and created/updated.

CLI can improve now:

- REST `Stack` already has draft revision ID, Git repo/ref fields, origin stack revision fields, and latest revision number. Expose these in get/vertical.
- Use `state` to combine status, outdated, draft, and EOL if available.

REST gaps:

- Stack DTO lacks owner/ownership display and EOL rollup.
- If list responses do not include current revision number/version directly, add a current revision summary to avoid extra requests.

Priority: Medium.

### Projects

Dashboard behavior:

- Project list shows name, memberships count, created, and updated.

Current CLI:

- `project list`: `id`, `name`, `title`.

Recommended CLI defaults:

- `project list`: `id`, `project`, `members`, `created`, `updated`.
- `project get`: include name/title, membership count, created/updated.

CLI can improve now:

- REST `Project` already has `createdAt` and `updatedAt`.

REST gaps:

- Project DTO lacks membership count.

Priority: Medium-low.

### Environments

Dashboard behavior:

- Environment list shows name, type, created, and updated.

Current CLI:

- `env list`: `id`, `name`, `title`, `type`.

Recommended CLI defaults:

- `env list`: `id`, `env`, `type`, `created`, `updated`.

CLI can improve now:

- REST `Env` already has `createdAt` and `updatedAt`.

REST gaps:

- No major gap.

Priority: Low.

### Members

Dashboard behavior:

- Membership views distinguish invited, joined, updated, status, and role.

Current CLI:

- `member list`: `id`, `member`, `email`, `role`, `status`, `joinedAt`.

Recommended CLI defaults:

- `member list`: `id`, `member`, `email`, `role`, `status`, `invited`, `joined`, `updated`.

CLI can improve now:

- REST `OrgMembership` already has `createdAt`, `updatedAt`, and `joinedAt`.

REST gaps:

- No major list-view gap.

Priority: Low.

### Organizations

Current CLI:

- `org list`: `id`, `name`, `title`, `domain`.

Recommended CLI defaults:

- Keep as-is, or add `created`/`updated` for `--wide` and get output.

CLI can improve now:

- REST `Org` already has `createdAt` and `updatedAt`.

REST gaps:

- No major list-view gap.

Priority: Low.

## REST API change matrix

### Fields already in REST but underused by CLI

- `Project.createdAt`, `Project.updatedAt`
- `Env.createdAt`, `Env.updatedAt`
- `OrgMembership.createdAt`, `OrgMembership.updatedAt`, `OrgMembership.joinedAt`
- `Org.createdAt`, `Org.updatedAt`
- `AppPort.publicPort`, `AppPort.updatedAt`
- `AppRoute.redirect*`, `AppRoute.updatedAt`
- `Task.progress`, `Task.projectIds`, `Task.originTaskId`, `Task.spawnedTaskIds`, `Task.repeatedTaskId`, `Task.startedAt`, `Task.endedAt`
- `TaskJob.logStatus`, `TaskJob.system`, `TaskJob.startedAt`, `TaskJob.endedAt`
- `TaskStep.logStatus`, `TaskStep.system`, `TaskStep.startedAt`, `TaskStep.endedAt`
- `Import.backupId`, `Import.startedAt`, `Import.endedAt`
- `AppServiceDeployment.jobName`, `AppServiceDeployment.skipPostDeployment`, `AppServiceDeployment.force`, `AppServiceDeployment.startedAt`, `AppServiceDeployment.endedAt`
- `Stack.gitRepoId`, `Stack.gitRepoRemoteId`, `Stack.gitRepoRef`, `Stack.gitRepoRefType`, `Stack.originStackRev*`, `Stack.draftRevId`, `Stack.latestRevNumber`
- `Cluster.hostname`, `Cluster.minNodeCount`, `Cluster.maxNodeCount`, `Cluster.lastNodeCount`, `Cluster.ips`, `Cluster.infraVersion`
- `AppInstance.stackRevNumber`, `AppInstance.stackVersion`, `AppInstance.stackName`, `AppInstance.stackTitle`, `AppInstance.stackIcon`

### REST fields to add first

App instances:

- `building`
- `needsRebuild`
- `needsRedeploy`
- `configurationReady`
- `configurationIssues`
- `outdated`
- latest build summary
- latest deployment summary
- last successful deployment timestamp
- CI integration summary
- registry integration summary
- owner/ownership summary
- EOL rollup across app services

App services:

- `eol`
- deployment summary
- last successful build summary
- storage usage
- configuration issue details
- image/build image summaries
- database relation summary
- service revision number/version summary

Builds:

- `imageVoidStatus`
- `canVoidImages`
- Git repository summary with slug and integration icon/name
- CI integration summary
- stack revision summary
- workflow
- pipeline
- commit author fields
- deployment summary

Deployments:

- stack revision summary
- post-deployment scripts status/list
- `skipPostDeploymentScripts`
- enabled service count or deployable service count

Backups:

- `title`
- integration summary
- `bucket`
- `storageClass`
- `url`
- `taskId`
- `startedAt`
- `endedAt`

Imports:

- `title`
- `url`
- `jobName`

Clusters:

- `country`
- `disableMonitoring`
- `outdated`
- external provider ID
- machine type
- node disk size
- allocatable CPU/memory/pods
- overhead CPU/memory/pods
- owner/ownership summary

Databases:

- `highAvailability`
- `replicas`
- `country`
- `external`
- owner/ownership summary
- private/public host and port
- SSL mode/status
- storage size
- IOPS
- machine type
- external provider ID
- resided cluster summary
- app/app instance/app service summaries
- integration summary

Integrations:

- provider revision summary
- provider kinds/types
- owner/ownership summary
- shared project summaries

Providers:

- current revision number/version
- manifest kinds/types summary
- maintainer/org summary
- source metadata

Catalog services:

- current revision number/version
- manifest-derived summary fields
- owner/source metadata
- EOL/build-support summary

Stacks:

- current revision summary if not included in list responses
- owner/ownership summary
- EOL rollup

Projects:

- membership count

Certificates:

- Align app certificate DTOs with org-level cert fields: key type, key length, issued, renews, expires.
- Add certificate usage count or usage summary.

## Suggested default columns

These are the default table columns I would move toward. Get/vertical output can include more fields, and JSON should stay complete.

| Command area | Suggested default columns |
| --- | --- |
| `org list` | `id`, `org`, `domain` |
| `member list` | `id`, `member`, `email`, `role`, `status`, `invited`, `joined`, `updated` |
| `project list` | `id`, `project`, `members`, `created`, `updated` |
| `env list` | `id`, `env`, `type`, `created`, `updated` |
| `app list` | `id`, `app`, `status`, `instances`, `state`, `updated` |
| `instance list` | `id`, `app`, `instance`, `env`, `stack`, `cluster`, `domain`, `state`, `deployed` |
| `app-service list` | `id`, `service`, `version`, `enabled`, `state`, `replicas`, `storage`, `lastDeploy` |
| `route list` | `id`, `service`, `route`, `action`, `cert`, `primary`, `private`, `state`, `updated` |
| `port list` | `id`, `service`, `name`, `port`, `publicPort`, `private`, `protocol`, `updated` |
| `cert list` | `id`, `host`, `issuer`, `key`, `used`, `status`, `issued`, `expires`, `renews` |
| `build list` | `id`, `number`, `service`, `images`, `repo`, `ref`, `stackRev`, `started`, `duration`, `status` |
| `deployment list` | `id`, `number`, `services`, `builds`, `postDeploy`, `stackRev`, `started`, `duration`, `status` |
| `backup list` | `id`, `target`, `backup`, `integration`, `bucket`, `status`, `started`, `duration` |
| `import list` | `id`, `target`, `import`, `source`, `status`, `started`, `duration` |
| `task list` | `id`, `title`, `status`, `progress`, `projects`, `author`, `started`, `duration` |
| `cluster list` | `id`, `cluster`, `integration`, `location`, `nodes`, `monitoring`, `state`, `created` |
| `database list` | `id`, `database`, `kind`, `version`, `env`, `source`, `location`, `state`, `created` |
| `integration list` | `id`, `integration`, `provider`, `scope`, `types`, `status`, `created`, `updated` |
| `provider list` | `id`, `provider`, `types`, `version`, `revision`, `status`, `maintainer`, `updated` |
| `catalog service list` | `id`, `service`, `type`, `version`, `revision`, `status`, `created`, `updated` |
| `stack list` | `id`, `stack`, `revision`, `version`, `state`, `source`, `created`, `updated` |

## Implementation plan

### Phase 1: CLI-only improvements

These do not require backend changes:

1. Add formatters for `route`, `location`, `source`, `enabled`, `revision`, and `state`.
2. Change task defaults to use `started`, `duration`, `progress`, and `projects`.
3. Change import defaults to use `started` and `duration`.
4. Add port `publicPort` and `updatedAt`.
5. Add project/env/member timestamps.
6. Add deployment `duration` and expose per-service deployment fields in get output.
7. Add stack source/origin/draft fields to get output.
8. Add pagination footer for table list responses.

### Phase 2: REST summary fields

Add small summary fields to REST list DTOs for the dashboard-equivalent views:

1. App instance summaries: state inputs, latest build/deploy, last deployed, owner, CI/registry, EOL.
2. App service summaries: EOL, deployment, storage, config issues, revision summary.
3. Build/deploy summaries: repo, stack revision, image/post-deploy status.
4. Backup/import summaries: title, storage/source detail, started/ended, task/log pointer.
5. Database/cluster summaries: location, owner, external/provider details, capacity/storage.

### Phase 3: Default column changes

After REST has the required fields, change default tables to the suggested column sets. Keep backward compatibility through:

- `-o json` for full payloads.
- `-o vertical` for detail fields.
- `--wide` or `--columns` for operators who need old/raw fields in table output.

## Highest-value changes to do first

1. Task output: REST already has most of the missing data, and this improves every workflow that launches operations.
2. Builds and deployments: change defaults to started/duration/status and add REST fields for repo, stack revision, image/post-deploy state.
3. App instance state: expose dashboard state inputs in REST and show one compact `state` column in CLI.
4. Backups/imports: add REST timing/storage/source fields so CLI can show started/duration and storage/source context.
5. Route/port cleanup: reduce route default width, add formatted route and public port.

These changes would make CLI read commands feel much closer to the dashboard while keeping the CLI appropriate for terminal use.
