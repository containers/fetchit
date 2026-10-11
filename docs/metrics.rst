Fleet metrics
=============

FetchIt provides an optional, read-only Prometheus endpoint for
`issue #418 <https://github.com/containers/fetchit/issues/418>`_. An existing
remote Prometheus server scrapes FetchIt; FetchIt does not push metrics or need
a Prometheus server URL. Deploying Prometheus is outside FetchIt's scope.

Enable the endpoint
-------------------

Set ``FETCHIT_METRICS_ADDR`` to an explicit listen address. Unset means no
metrics listener. Port ``8080`` is the convention used in these examples;
you can choose another port. For a native process, begin with loopback:

.. code-block:: bash

   FETCHIT_METRICS_ADDR=127.0.0.1:8080 FETCHIT_HOSTNAME=edge-01.example.invalid fetchit start
   curl -i http://127.0.0.1:8080/metrics

``FETCHIT_HOSTNAME`` defaults to the operating system hostname. For containers,
set it to the underlying host's unique hostname, since the container hostname
usually identifies the container. Allowed values contain 1–253 ASCII letters,
digits, dots, underscores or hyphens, starting with a letter or digit. An invalid
value fails startup. Host identity comes from configuration, never request
headers. ``/metrics`` includes the ``hostname`` label and an
``X-Fetchit-Hostname`` response header. The separate :doc:`status` listener
also includes that header, and ``/status`` adds a top-level ``hostname`` field.

For an existing container deployment, add these options to ``podman run``:

.. code-block:: bash

   -e FETCHIT_METRICS_ADDR=:8080 \
   -e FETCHIT_HOSTNAME=edge-01.example.invalid \
   -p 127.0.0.1:8080:8080

Keep the deployment's existing config, state volume, and Podman socket mounts.
These options listen inside the container and publish only the host's loopback
port. With host networking, omit ``-p`` and bind ``127.0.0.1:8080`` directly.

Remote collection and security
------------------------------

To scrape remotely, publish on the host's monitoring-network address instead
of loopback, or bind that address directly with host networking. For example,
if ``10.0.1.20`` is an address assigned to the host:

.. code-block:: bash

   -e FETCHIT_METRICS_ADDR=:8080 \
   -e FETCHIT_HOSTNAME=edge-01.example.invalid \
   -p 10.0.1.20:8080:8080

Permit inbound TCP 8080 only from the collector using the host firewall and
network security rules. In AWS, restrict the instance security group's rule to
the collector's security group or a specific private source address. Verify
both intended access and denial from other hosts. A private address alone is
not access control. Loopback does not authenticate local processes; published
loopback ports do not isolate a service from container-network peers.

The listener serves plain HTTP with no authentication or native TLS. Use only
on a deliberately restricted trusted monitoring network. For traffic crossing
untrusted networks, use an authenticated TLS proxy or tunnel, with certificate
verification at the collector. This endpoint supplies neither encryption nor
credentials. Do not publish it on the public internet.

The metrics listener serves only ``GET /metrics`` (also ``HEAD``). It does not
expose ``/status``, ``/healthz``, debug, or administration routes. Leave
``FETCHIT_STATUS_ADDR`` unset unless its separate endpoint is needed:
``/status`` contains raw method names and repository URLs. The two listeners
must use different addresses/ports if both are enabled.

Scrapes read an in-memory snapshot. They do not fetch Git, invoke Podman, or
change workloads. The output contains bounded kinds/states and the configured
hostname; no repository URLs, method names, paths, SHAs, credentials, decrypted
contents, or free-form errors become labels. A Git SHA identifies a revision
shared by many hosts, not a host identity. Keep hostnames unique within the
fleet; Prometheus also attaches its own ``job`` and ``instance`` target labels.

Configure an existing Prometheus server:

.. code-block:: yaml

   scrape_configs:
     - job_name: fetchit
       metrics_path: /metrics
       scrape_interval: 30s
       static_configs:
         - targets: ['edge-01.example.invalid:8080']

Replace the target with the reachable, access-controlled endpoint. Prometheus's
own port ``9090`` does not determine FetchIt's listening port.

Metrics and synchronization
---------------------------

All metrics are gauges. A configuration means a **method declaration**, not a
repository, file, or deployed resource. One repository may contain several
independently scheduled methods. Counts describe the current configuration;
removed methods disappear and reloads reset synchronization to unknown until
startup replay and reconciliation complete.

.. list-table:: Metric families
   :header-rows: 1
   :widths: 45 55

   * - Metric
     - Meaning and labels
   * - ``fetchit_host_info``
     - Value 1; ``hostname``.
   * - ``fetchit_configured_methods``
     - All configured method declarations; ``hostname``, ``kind``.
   * - ``fetchit_git_methods``
     - Connected Git methods; ``hostname``, ``kind``, ``state``.
   * - ``fetchit_git_oldest_observation_timestamp_seconds``
     - Oldest successful Git observation among methods of a kind, in Unix
       seconds; ``hostname``, ``kind``. Zero if any has never observed Git.

``kind`` is one of ``raw``, ``kube``, ``ansible``, ``filetransfer``, ``systemd``,
``quadlet``, ``image``, ``prune``, ``config`` or ``other``. Only kinds
present in configuration are emitted. For connected Git methods, all three
state series are emitted, including zero values:

* ``synced``: the latest successfully fetched and accepted branch revision was
  applied successfully, and the saved Git baseline matches it. Startup replay
  must also succeed. An unchanged revision is confirmed by a successful check.
* ``out_of_sync``: a desired revision was observed but its application or
  baseline update has not completed successfully. Partial application, failed
  application, skipped bad commits, and rollback do not establish sync.
* ``unknown``: no completed observation/application establishes the current
  state, including startup, checks in progress, clone/replay failure, or source
  access failure. A cancelled run cannot report success.

Image, prune, config reload, auto-update, and disconnected archive/device
methods count toward configured declarations, but not connected Git sync
counts. Disconnected operation cannot establish whether a remote Git branch
is current. Mirror-enabled targets observe the accepted configured source
using FetchIt's existing mirror selection and commit verification rules.

Git is checked on each method's schedule. ``synced`` means up to date with the
**last observed** repository/branch revision; commits published afterward are
unknown until the next check. Freshness advances only after a successful Git
fetch/reference checkout and any required commit verification. An apply failure
can have fresh Git knowledge while remaining out of sync. Source failure keeps
the previous timestamp, or zero if none exists. This is configuration
synchronization, not workload readiness or detection of manual workload drift.

Example output for one successful raw method:

.. code-block:: text

   fetchit_host_info{hostname="edge-01.example.invalid"} 1
   fetchit_configured_methods{hostname="edge-01.example.invalid",kind="raw"} 1
   fetchit_git_methods{hostname="edge-01.example.invalid",kind="raw",state="synced"} 1
   fetchit_git_methods{hostname="edge-01.example.invalid",kind="raw",state="out_of_sync"} 0
   fetchit_git_methods{hostname="edge-01.example.invalid",kind="raw",state="unknown"} 0
   fetchit_git_oldest_observation_timestamp_seconds{hostname="edge-01.example.invalid",kind="raw"} 1791700000.000

Queries and alert examples
--------------------------

Total configured declarations per host:

.. code-block:: promql

   sum by (hostname) (fetchit_configured_methods)

Pending or unknown connected Git methods:

.. code-block:: promql

   sum by (hostname) (fetchit_git_methods{state=~"out_of_sync|unknown"}) > 0

For methods checking every minute, a five-minute observation deadline can be a
starting point. Adjust for the longest configured interval, skew, and expected
operation duration. Require the condition to persist to avoid alerts during
normal checks or startup:

.. code-block:: yaml

   groups:
     - name: fetchit
       rules:
         - alert: FetchitGitNotSynced
           expr: sum by (hostname) (fetchit_git_methods{state=~"out_of_sync|unknown"}) > 0
           for: 5m
         - alert: FetchitGitObservationStale
           expr: time() - fetchit_git_oldest_observation_timestamp_seconds > 300
           for: 5m
         - alert: FetchitScrapeUnavailable
           expr: up{job="fetchit"} == 0
           for: 2m

A successful scrape (``up == 1``) proves endpoint availability, not successful
Git observation or application. Zero freshness is deliberately stale. Missing
series after removal are different from a zero-valued state series; the
``up`` alert detects a failed scrape, while removal of a target from Prometheus
requires inventory monitoring outside FetchIt.

References
----------

See `Prometheus scraping <https://prometheus.io/docs/prometheus/latest/getting_started/>`_,
the `security model <https://prometheus.io/docs/operating/security/>`_, and
`jobs and instances <https://prometheus.io/docs/concepts/jobs_instances/>`_.
