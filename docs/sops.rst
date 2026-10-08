Encrypted Kube manifests with SOPS
==================================

FetchIt can decrypt SOPS-encrypted YAML when deploying a Kube method. This is
optional; methods without a ``sops`` block keep their existing behavior. FetchIt's
images include SOPS 3.13.3 for amd64 and arm64, installed with pinned checksums.
SOPS is distributed under MPL-2.0; its license is included at
``/usr/share/licenses/sops/LICENSE`` and its upstream source is available at
https://github.com/getsops/sops/tree/v3.13.3. Only local age recipients are supported. Cloud KMS, PGP, key groups, external key
services, Raw, Quadlet, and encrypted FetchIt configuration are not supported.

Prepare an encrypted repository
-------------------------------

Install SOPS and age on your authoring machine. Generate an age identity outside
the repository, protect it, and obtain its public recipient:

.. code-block:: shell

   install -d -m 0700 "$HOME/.config/fetchit"
   age-keygen -o "$HOME/.config/fetchit/age.txt"
   chmod 0600 "$HOME/.config/fetchit/age.txt"
   age-keygen -y "$HOME/.config/fetchit/age.txt"

Keep the private identity file outside Git. The public recipient can be committed
in a repository's ``.sops.yaml``:

.. code-block:: yaml

   creation_rules:
   - path_regex: '\.enc\.ya?ml$'
     encrypted_regex: '^(data|stringData)$'
     age: age1REPLACE_WITH_YOUR_PUBLIC_RECIPIENT

Create a manifest containing a Secret and its Pod. Use names dedicated to this
FetchIt method; kube down deletes Secret documents as well as workloads, so these
secrets must not be shared with workloads managed elsewhere.

.. code-block:: yaml

   apiVersion: v1
   kind: Secret
   metadata:
     name: application-secret
   stringData:
     password: replace-me
   ---
   apiVersion: v1
   kind: Pod
   metadata:
     name: application
   spec:
     containers:
     - name: web
       image: docker.io/library/alpine:3.22
       command: [sleep, infinity]
       env:
       - name: PASSWORD
         valueFrom:
           secretKeyRef:
             name: application-secret
             key: password

Save plaintext outside the repository and encrypt it into the tracked directory:

.. code-block:: shell

   mkdir -p kube
   sops encrypt --filename-override kube/application.enc.yaml \
     --input-type yaml --output-type yaml \
     < /secure/path/application.yaml > kube/application.enc.yaml
   git add .sops.yaml kube/application.enc.yaml
   git commit -m 'Deploy encrypted application'

Use the YAML extension, not a binary SOPS store. SOPS encrypts the multi-document
manifest as a whole and supplies integrity metadata for each document. Do not
split, concatenate, or edit encrypted documents by hand. The example encrypts
``data`` and ``stringData``; resource names and other manifest fields remain
visible and authenticated by the SOPS MAC. ``mac_only_encrypted: true`` is
rejected because resource identities must also be authenticated. A method with ``sops`` requires encrypted input for every selected YAML
file, including manifests that contain only public fields. Separate plaintext
manifests into an ordinary Kube method or filter encrypted files with ``glob``.

Configure FetchIt
-----------------

.. code-block:: yaml

   targetConfigs:
   - url: https://github.com/example/workloads
     branch: main
     kube:
     - name: application
       targetPath: kube
       glob: '**.enc.yaml'
       schedule: '*/1 * * * *'
       sops:
         ageKeyFile: /run/secrets/fetchit-age-keys

The key path must be absolute inside FetchIt's container, readable, a regular
file no larger than 64 KiB, and inaccessible to group/other users (normally
0400 or 0600). An empty ``sops`` block is invalid. FetchIt rejects a key file
resolved inside its repository checkout. Mount the host file read-only, for
example by adding these arguments to the normal FetchIt container command:

.. code-block:: shell

   -v "$HOME/.config/fetchit/age.txt:/run/secrets/fetchit-age-keys:ro,Z"

The ``Z`` suffix supplies a private SELinux label on Fedora. Ensure that the
container user can read the mounted file without widening its permissions. Mount
only the private key file, not your whole credentials directory. Alternatively,
provision the identity through a Podman file secret, mounted at the configured
path with private permissions. FetchIt does not inherit ambient age identities,
Git/cloud credentials, or SOPS key-service settings into its decryption process.
For a custom image or a native binary, install the supported SOPS executable at
``/usr/local/bin/sops``. Existing optional ``networks`` settings remain supported.

Updates, deletion, and recovery
-------------------------------

FetchIt decrypts and validates all changed old/new manifests in a Kube method
before stopping any workloads. Missing/wrong keys, authentication failure,
malformed manifests, or failed network preflight stop that attempt and leave
running workloads and the applied commit unchanged. Errors report safe failure
categories; plaintext and upstream decryption diagnostics are not logged.

Updates and renames remove the old resources and play the new manifest. Deleting
a tracked encrypted manifest decrypts its previous Git version to remove its
resources. Retain every old age identity required by the currently applied Git
version, including for deletion. During key rotation, provision a file containing
both old and new identities, re-encrypt and commit the manifests for the new
recipient, wait for successful deployment, then remove the old identity. Changing
the key file alone does not redeploy unchanged manifests. Restore the required
identity and FetchIt retries on the next scheduled attempt.

Each encrypted input and decrypted output is limited to 8 MiB. Prepared plaintext
for one apply attempt is limited to 32 MiB. Each SOPS invocation has a 30-second
deadline. Split larger workloads across methods. Decrypted manifests are sent
through the Podman API from memory; FetchIt does not write plaintext manifests to
Git checkouts or temporary files. Memory clearing is best effort, not a guarantee
against process/host access. Values also reach Podman and its secret store;
SOPS protects Git content, not a compromised deployment host.

Preparation is not a Podman transaction. Runtime failures after teardown can
interrupt deployment or leave some changes applied; the applied commit is not
advanced and subsequent scheduled attempts retry. Do not share resource or
secret names across methods. SOPS methods use body-based remote kube play and
support self-contained YAML; automatic Containerfile builds and local auxiliary
files are outside this feature's supported scope. Existing plaintext methods
continue to use their existing path-based behavior.

If decryption fails, check the key mount, permissions, supported age metadata,
integrity, file sizes, and executable installation without printing secrets to
logs. If Podman play fails, check resource names, images, supported Podman 5 YAML
fields, and network availability. Provisioning workload secrets outside Git
remains an alternative; existing Git authentication secrets are independent of
this feature.
