Run the production image against a temporary, loopback-only smart HTTP Git
repository and real rootful Podman:

```sh
sudo systemctl enable --now podman.socket
sudo podman pull quay.io/fetchit/fetchit-sample-app:latest
sudo env FETCHIT_TEST_ENGINE_IMAGE=localhost/fetchit:metrics \
  bash tests/integration/metrics/test-metrics.sh
```

Build the engine using the repository Dockerfile and vendored dependencies
first. The test needs Bash, Python 3, Git, curl, awk, and a free TCP port 8080.
It removes its own engine, workload, state volume, Git server, and temporary
files on exit. Allow roughly five minutes for scheduled checks.

The test verifies scheduled Git updates, an invalid workload remaining out of
sync, source failure retaining the previous observation timestamp, recovery,
hostname reporting, absence of repository metadata, rejected administration
routes, and read-only scrapes.

For a collector on a second private host, override
`FETCHIT_TEST_METRICS_ADDR=PRIVATE_HOST_IP:8080` and
`FETCHIT_TEST_METRICS_URL=http://PRIVATE_HOST_IP:8080/metrics`. Restrict inbound
8080 to that collector before running. Configure the existing collector to
scrape that address. The test neither installs nor starts Prometheus.
