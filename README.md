# domain-exporter

`domain-exporter` is a Go-based tool that exposes Prometheus metrics to monitor the remaining time before configured domains expire.

## Features

- Load configuration from a YAML file.
- Expose metrics in Prometheus format.

### Available Flags

- `-address`: Address to bind the HTTP server (default `:8080`).
- `-config`: Path to the YAML configuration file (default `config.yaml`).

### Environment Variables

- `ADDRESS`: Address to bind the HTTP server (optional).
- `CONFIG_PATH`: Path to the YAML configuration file (optional).

## Configuration

The configuration file must be in YAML format and include a list of domains with their expiration dates. Example:

```yaml
domains:
  - domain: "example.com"
    expires: "2025-12-31"
  - domain: "example.org"
    expires: "2026-01-15"
```

## Metrics

The exporter exposes the following metrics at `/metrics`:

- `domain_days_to_expire`: Days remaining before a domain expires. Labels:
    - `domain`: The domain name.

## Example Execution

```bash
./domain-exporter -address ":9090" -config "domains.yaml"
```

Access the metrics at: [http://localhost:9090/metrics](http://localhost:9090/metrics)

## Why not use whois to get the expiration date?

In my case I manage some domains with a TLD that does not provide the expiration date in the whois response, so I just
use the metrics to get the expiration date and know when to update the configuration file.

## License

This project is licensed under the MIT License. See the `LICENSE` file for details.
