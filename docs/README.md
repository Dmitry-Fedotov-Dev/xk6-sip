# Documentation

`sources/k6/next/javascript-api/k6-x-sip/` is the reference of the `k6/x/sip`
JavaScript API, written in the format of
[grafana/k6-docs](https://github.com/grafana/k6-docs): Hugo pages with front
matter, `{{< code >}}` blocks and one page per method. The folder can be
copied as is to `docs/sources/k6/next/javascript-api/` of k6-docs.

- [k6/x/sip](sources/k6/next/javascript-api/k6-x-sip/_index.md) – concepts, lifecycle, module functions
- [Device](sources/k6/next/javascript-api/k6-x-sip/device/_index.md) – subscriber options and methods
- [Call](sources/k6/next/javascript-api/k6-x-sip/call/_index.md) – call control, media, hold, transfers
- [Metrics](sources/k6/next/javascript-api/k6-x-sip/metrics.md)

Examples are marked `<!-- md-k6:skip -->`, because the k6-docs CI runs
examples with a stock k6 binary that doesn't include this extension.
