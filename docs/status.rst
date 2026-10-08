Health and status endpoint
==========================

FetchIt can expose an optional HTTP endpoint for process liveness and scheduled
method activity, including Quadlet methods. Set ``FETCHIT_STATUS_ADDR`` to enable
it; leaving the variable unset opens no listener. For a host installation, use
``FETCHIT_STATUS_ADDR=127.0.0.1:8080`` for local access.

For a container, add these options to the normal FetchIt launch command:

.. code-block:: shell

   -e FETCHIT_STATUS_ADDR=:8080 -p 127.0.0.1:8080:8080

``:8080`` listens on all interfaces inside the container. Publishing on the
host's loopback address restricts host access. The endpoint has no authentication
and exposes method names and repository URLs; use trusted access or an
authenticated reverse proxy for remote monitoring. Repository URLs should not
contain credentials.

.. code-block:: shell

   curl --fail http://127.0.0.1:8080/healthz
   curl --fail http://127.0.0.1:8080/status

``GET /healthz`` returns ``200 OK`` with ``ok`` while the HTTP server can respond.
It is a liveness check: it does not assert Podman connectivity, successful
reconciliation, or workload readiness.

``GET /status`` returns JSON such as:

.. code-block:: json

   {
     "status": "running",
     "startedAt": "2026-10-08T12:00:00Z",
     "uptimeSeconds": 312,
     "methods": [
       {
         "kind": "quadlet",
         "name": "web-example",
         "url": "https://github.com/example/workloads",
         "schedule": "*/1 * * * *",
         "runs": 5,
         "lastRun": "2026-10-08T12:05:00Z"
       }
     ]
   }

Names are the method's internal identity and may include a configuration hash.
``runs`` counts scheduled invocation attempts; ``lastRun`` is the invocation
start time, before any configured skew delay. These fields do not indicate
success or completion; inspect FetchIt logs and systemd for reconciliation
results. Methods that have not run omit ``lastRun``. Methods without a Git
repository omit ``url``.

Config reloads remove retired methods, update schedules, and preserve counters
for identities that remain configured. A process restart resets all statistics.
``startedAt`` is initialized when the first configuration is registered, including
an empty configuration. Before initialization, the response reports
``status: initializing``, zero uptime, and omits ``startedAt``. The listener starts
once per process; changing its address requires restarting FetchIt. Bind failures
are logged and require a restart after correcting the address or port conflict.

HTTP read, header, write, and idle timeouts limit slow connections. Only GET and
HEAD are accepted for these endpoints.
