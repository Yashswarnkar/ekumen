# Build map prototype

A stand-in for `ekumen map`, used by the orchestrator while `.ekumen/` is still maintained by hand. `gen.rb` reads `.ekumen/registry.yaml`, `.ekumen/decisions/*.yaml` and `gh pr list`, and fills `template.html` with that data. The template computes status, priority and the graph layout (dagre) in the browser.

```
ruby tools/map-prototype/gen.rb . tools/map-prototype/template.html /tmp/ekumen-map.html
```

It is reference material for T-cli, which replaces it with `ekumen map`. Delete this folder when that ships.
