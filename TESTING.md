# Testing

How the plugin is tested, and in particular how we check that streaming (gRPC) and non-streaming (HTTP) queries behave the same.

## Run the tests

| What                                        | Command                                                                                              | Needs                                                                                      |
| ------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| Frontend unit tests                         | `npm run test:ci`                                                                                    | nothing                                                                                    |
| Backend unit tests and the fake-Tempo tests | `go test -race ./pkg/...`                                                                            | nothing                                                                                    |
| Generated queries against a real Tempo      | `DIFF_TEMPO_URL=http://localhost:3200 go test ./pkg/tempo -run TestDifferential_GeneratedQueries -v` | `docker compose up -d tempo db app loadgen`, then wait about 5 minutes for traces          |
| End-to-end tests                            | `npx playwright test --project=chromium`                                                             | `docker compose up -d`, a built plugin, and the trace generators running for a few minutes |

CI runs all of them: `go test -race ./pkg/...` and the Jest tests in "Test and build plugin", the Playwright matrix over the supported Grafana versions, and the real-Tempo test in the "Streaming differential tests (real Tempo)" job.

## Streaming and non-streaming differential tests

Search and TraceQL metrics queries reach Tempo over HTTP when streaming is off and over gRPC when it is on. The two paths build their requests, forward headers and shape results in separate code, so they can drift apart. The tests send the same query down both paths and compare what comes back, so they find a difference without needing to know the right answer.

There are three layers. They are not interchangeable: each one sees something the others cannot.

| Layer                                  | Where                                                                                                     | Tempo is                                                     | It can see                                                                                                                                                                                                                              | It cannot see                                                                                                  |
| -------------------------------------- | --------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| Fake-Tempo tests                       | `pkg/tempo/streaming_differential_harness_test.go`, `lbac_forwarding_test.go`, `streaming_parity_test.go` | a fake that records every request                            | exactly what the plugin sends (query, time range, limit, step, exemplars) and with which headers (team policy, basic auth, custom headers); duplicate and late messages; errors and an unreachable Tempo on demand; concurrent requests | how a real Tempo behaves; it only agrees with itself                                                           |
| Generated queries against a real Tempo | `pkg/tempo/streaming_differential_generated_test.go`                                                      | the Tempo from `docker-compose.yaml`, with the demo traces   | whether the two paths agree on real responses, for many queries nobody wrote by hand                                                                                                                                                    | headers, auth and team policy (the compose Tempo has none); exemplars (sampled); anything it does not generate |
| End-to-end tests                       | `tests/e2e/streamingDifferential.spec.ts`                                                                 | the same real Tempo, through Grafana and the packaged plugin | the frontend, Grafana's own handling of the stream, and the Grafana versions we support                                                                                                                                                 | the details above; it runs only three queries                                                                  |

### Why we need both a fake and a real Tempo

We checked it by injecting one-line bugs into the plugin and running the tests. These numbers are a snapshot from November 2026 (53 faults in the request, header and result code of both paths); rerun the experiment before relying on them after big changes.

- **The fake is needed.** Before the fake-Tempo tests existed, 21 of the first 50 faults passed every test: dropped basic auth over gRPC, an HTTP client that stopped forwarding the team policy, duplicate traces not removed, exemplars dropped, and most streaming request-construction faults. A real Tempo cannot show any of these. It ignores the policy header, it returns what it was asked for whatever the request looked like, and it does not send duplicates on demand.
- **A real Tempo adds something else.** For these faults it found nothing the fake does not: the other tests catch all 53, and the generated test alone catches 40 of them, all also caught by the others. What it checks is whether our assumptions about Tempo's real responses hold, and a fault list cannot express that. So far it found two differences between the paths that nothing else did: the HTTP path truncates metrics time ranges to whole seconds while streaming sends nanoseconds, and the HTTP table shows only the first span set of a grouped search while streaming shows all of them (see below).
- **The fake and the real Tempo check each other.** The fake's picture of Tempo comes from us. The real-Tempo test runs in CI on pull requests, including the ones that change the pinned Tempo image in `docker-compose.yaml`, so a Tempo that starts returning something the fake does not model shows up as a failure.

If you can state exactly what the plugin should send, put the test in the fake-Tempo harness. If the question is whether both paths cope with whatever a real Tempo returns, extend the generators in the real-Tempo test.

### Known differences

The tests work around these instead of failing on them. The generated test logs them as `known gap`.

- **Metrics time ranges** ([#312](https://github.com/grafana/grafana-tempo-datasource/issues/312)). The HTTP path truncates them to whole seconds, streaming sends nanoseconds, so a range with a sub-second part (which is what Grafana sends) can give slightly different values. The fake-Tempo and e2e tests use whole-second ranges.
- **Several span sets** ([#314](https://github.com/grafana/grafana-tempo-datasource/issues/314)). For a grouped search (`| by(...)`) Tempo sends the deprecated `spanSet` (the first group) and `spanSets` (all of them). The frontend builds the streamed table from `spanSets`, the Go code of the HTTP path from `spanSet` when it is present, so the HTTP table shows only the first group.
- **Forwarded request headers.** Grafana does not apply its OAuth token and cookies middlewares to streams, so OAuth passthrough and cookies only reach Tempo over HTTP. This is a Grafana limitation, not a plugin one.

A related bug the generated test does not reach: a span attribute that is an array makes the HTTP table builder panic while streaming returns the trace ([#315](https://github.com/grafana/grafana-tempo-datasource/issues/315)). The demo data has no array attributes, so it was found by sending one span to Tempo by hand. Extend the generators, or the demo data, when you learn of a shape the demo traces lack.

### Reading a failure of the real-Tempo test

The test prints the seed and the case: rerun it with `DIFF_SEED=<seed> DIFF_CASES=<n>` to get the same queries. The seed fixes the queries, not the time windows: they are taken from the current time (windows of 30 to 120 seconds that ended 90 to 150 seconds ago), so a rerun uses slightly different windows over the same data. It fails if Tempo has too few recent traces, and also if fewer than half of the compared cases of a kind return any data, because two paths that both return nothing always agree. The windows stay inside the last 270 seconds because the CI job only waits for traces from 4 to 5 minutes ago; widen them only together with that wait. A case where only one path times out is run once more before it counts.

## Checking that a test can fail

A test that cannot fail proves nothing. After adding or changing one, break the code it is meant to protect (swap two arguments, drop a header, change a unit), run the suite, and check that the test fails. Restore the code afterwards. We did this for the tests above with 53 request, header and result faults and 34 team-policy (LBAC) faults; the results are in the description of the pull requests that added them.
