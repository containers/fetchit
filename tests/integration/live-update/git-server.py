#!/usr/bin/env python3
"""Loopback-only, read-only smart HTTP Git fixture for the live-update test."""

import argparse
import os
from pathlib import Path
import subprocess
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlsplit


class GitHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.serve_git()

    def do_POST(self):
        self.serve_git()

    def serve_git(self):
        request = urlsplit(self.path)
        if request.path not in ("/live.git/info/refs", "/live.git/git-upload-pack"):
            self.send_error(404)
            return
        length = int(self.headers.get("Content-Length", "0"))
        if length < 0 or length > 16 * 1024 * 1024:
            self.send_error(413)
            return
        environment = os.environ.copy()
        environment.update(
            GIT_PROJECT_ROOT=self.server.repository_root,
            GIT_HTTP_EXPORT_ALL="1",
            REQUEST_METHOD=self.command,
            PATH_INFO=request.path,
            QUERY_STRING=request.query,
            CONTENT_TYPE=self.headers.get("Content-Type", ""),
            CONTENT_LENGTH=str(length),
            REMOTE_ADDR=self.client_address[0],
            HTTP_GIT_PROTOCOL=self.headers.get("Git-Protocol", ""),
        )
        try:
            response = subprocess.run(
                ["git", "http-backend"],
                input=self.rfile.read(length),
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                env=environment,
                timeout=30,
                check=True,
            ).stdout
            headers, body = response.split(b"\r\n\r\n", 1)
        except (subprocess.SubprocessError, ValueError):
            self.send_error(502, "Git fixture backend failed")
            return
        status = 200
        outgoing = []
        for line in headers.decode("ascii").splitlines():
            key, value = line.split(":", 1)
            if key.lower() == "status":
                status = int(value.strip().split()[0])
            else:
                outgoing.append((key, value.strip()))
        self.send_response(status)
        for key, value in outgoing:
            self.send_header(key, value)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("repository_root")
    parser.add_argument("port_file")
    args = parser.parse_args()
    with HTTPServer(("127.0.0.1", 0), GitHandler) as server:
        server.repository_root = str(Path(args.repository_root).resolve())
        Path(args.port_file).write_text(str(server.server_port))
        server.serve_forever()
