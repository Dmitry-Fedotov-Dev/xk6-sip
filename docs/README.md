# Documentation

Reference of the `k6/x/sip` JavaScript API, one page per method, with
parameters, return values and an example:

- [k6/x/sip](sources/k6/next/javascript-api/k6-x-sip/_index.md) – concepts, lifecycle, module functions
- [Device](sources/k6/next/javascript-api/k6-x-sip/device/_index.md) – subscriber options and methods
- [Call](sources/k6/next/javascript-api/k6-x-sip/call/_index.md) – call control, media, hold, transfers, `trace()`
- [Metrics](sources/k6/next/javascript-api/k6-x-sip/metrics.md)
- [Monitoring](sources/k6/next/javascript-api/k6-x-sip/monitoring.md) – Prometheus and Grafana stack, dashboard, how to read it

## Export to k6-docs

The pages follow the layout of [grafana/k6-docs](https://github.com/grafana/k6-docs)
but are kept in plain GitHub Markdown here, so they render on GitHub.
`docs/export` converts them to the k6-docs Hugo format: links to `.md` files
become page URLs and examples are wrapped in `{{< code >}}`.

```sh
go run ./docs/export ../k6-docs/docs/sources/k6/next/javascript-api
```

Examples are marked `<!-- md-k6:skip -->`, because the k6-docs CI runs
examples with a stock k6 binary that doesn't include this extension.
