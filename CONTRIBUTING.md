# Contributing

Thank you for your interest in contributing to the Tempo data source for Grafana! We welcome contributions from the community.

Feel free to [browse open issues](https://github.com/grafana/grafana-tempo-datasource/issues) or open a new one. For more general guidance, see [Grafana's Contributing Guide](https://github.com/grafana/grafana/blob/main/CONTRIBUTING.md).

This project adheres to the [Grafana Code of Conduct](https://github.com/grafana/grafana/blob/main/CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code.

## Prerequisites

- [Git](https://git-scm.com/)
- [Go](https://golang.org/dl/) (see [go.mod](go.mod) for the minimum required version)
- [Mage](https://magefile.org/)
- [Node.js LTS](https://nodejs.org)
- [npm](https://docs.npmjs.com/downloading-and-installing-node-js-and-npm) (see [package.json](package.json) for the minimum required version)
- [Docker](https://docs.docker.com/get-docker/)

## Frontend

1. Install dependencies:

   ```shell
   npm install
   ```

2. Build plugin in development mode and watch for changes:

   ```shell
   npm run dev
   ```

3. Build plugin in production mode:

   ```shell
   npm run build
   ```

4. Run frontend tests:

   ```shell
   npm run test:ci
   ```

## Backend

1. Build the backend binaries:

   ```shell
   mage -v
   ```

## Local development environment

`npm run server` starts a local Tempo instance and a Grafana instance with the plugin pre-provisioned:

```shell
npm run server
```

The local stack mirrors `grafana/grafana`'s `devenv/docker/blocks/tempo` setup so the
plugin always has trace data to query during development:

| Service      | Image                         | What it does                                             | Host port               |
| ------------ | ----------------------------- | -------------------------------------------------------- | ----------------------- |
| `tempo`      | `grafana/tempo:latest`        | Trace backend (OTLP gRPC/HTTP, Jaeger HTTP, query)       | 3200, 4317, 4318, 14268 |
| `prometheus` | `prom/prometheus:latest`      | Receives Tempo's metrics-generator remote_write          | 9090                    |
| `db`         | `grafana/tns-db:9c1ab38`      | TNS demo app — emits Jaeger traces via `JAEGER_ENDPOINT` | 8000                    |
| `app`        | `grafana/tns-app:9c1ab38`     | TNS demo app                                             | 8001                    |
| `loadgen`    | `grafana/tns-loadgen:9c1ab38` | Drives traffic against `app` so traces flow continuously | 8002                    |

Tempo is reachable at `http://localhost:3200` and accepts OTLP traces on ports `4317`
(gRPC) / `4318` (HTTP). The TNS apps push traces over Jaeger to `tempo:14268`, and Tempo's
metrics-generator (`service-graphs`, `span-metrics`, `local-blocks`) remote-writes to
Prometheus so the Service Graph and span-metrics views in the Tempo datasource have data
out of the box. Both datasources are pre-provisioned (`Tempo` is default, `Prometheus`
backs the Service Graph and exemplar trace IDs link back to Tempo).

## E2E tests

```shell
npm run server
npm run e2e
```

Or, to install Playwright browsers first:

```shell
npx playwright install --with-deps
npm run server
npm run e2e
```

## Release

You need commit access to the repository to publish a release.

1. Update the version number in `package.json`.
2. Update `CHANGELOG.md` with the changes included in the release.
3. Open a PR with the changes and merge it.
4. Follow the release process described [here](https://enghub.grafana-ops.net/docs/default/component/grafana-plugins-platform/plugins-ci-github-actions/010-plugins-ci-github-actions/#cd_1).
