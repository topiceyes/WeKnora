#!/usr/bin/env python3
"""Exercise the embed hardening and MCP Nginx locations with backend fixtures.

The site deliberately serves no iframe-embed surface: /embed/*, /embed.html
and /weknora-widget.js must return 404 even when the files exist in the
webroot (an explicit `return 404` beats the SPA fallback). MCP proxying is
covered end-to-end. Run: python3 scripts/test_embed_nginx.py [nginx-image]
"""
import json
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
IMAGE = sys.argv[1] if len(sys.argv) > 1 else "nginx:alpine"


def docker(*args):
    return subprocess.check_output(["docker", *args], text=True).strip()


def check():
    with tempfile.TemporaryDirectory(prefix="weknora-embed-nginx-") as folder:
        work = Path(folder)
        (work / "web").mkdir()
        (work / "web/embed.html").write_text("embed entry")
        (work / "web/weknora-widget.js").write_text("embed entry")
        (work / "web/index.html").write_text("main SPA")
        (work / "web/50x.html").write_text("backend unavailable")
        config = (ROOT / "frontend/nginx.conf").read_text()
        for key, value in {
            "APP_SCHEME": "http", "APP_HOST": "127.0.0.1", "APP_PORT": "18081",
            "MAX_FILE_SIZE": "50m", "MAX_SKILL_BUNDLE_SIZE": "100m",
        }.items():
            config = config.replace("${" + key + "}", value)
        config = config.replace("/usr/share/nginx/html", "/check/web")
        config = config.replace("/etc/nginx/api-proxy.conf", "/check/api-proxy.conf")
        (work / "api-proxy.conf").write_text((ROOT / "frontend/nginx-api-proxy.conf").read_text())
        # The token middleware and MCP auth are covered by Go tests. Here vary
        # the fixture responses to verify proxying and header pass-through.
        fixture = """
        server {
            listen 18081;
            location /api/ { return 200 "$http_host"; }
            location /mcp/ {
                default_type application/json;
                if ($http_authorization != "Bearer test-token") { return 401; }
                add_header Mcp-Session-Id "test-session";
                return 200 '{"method":"$request_method","path":"$request_uri","host":"$http_host","session":"$http_mcp_session_id","protocol":"$http_mcp_protocol_version"}';
            }
            location = /mcp/stream {
                default_type text/event-stream;
                return 200 "event: message\\ndata: test-event\\n\\n";
            }
        }
        """
        (work / "nginx.conf").write_text(
            "events {}\nhttp {\n" + (ROOT / "frontend/nginx-http.conf").read_text() + config + fixture + "\n}"
        )
        # The Nginx worker runs as an unprivileged user in the container.
        work.chmod(0o755)
        cid = docker("create", "-p", "127.0.0.1::80",
                     "--entrypoint", "nginx", IMAGE, "-c", "/check/nginx.conf", "-g", "daemon off;")
        try:
            docker("cp", str(work), cid + ":/check")
            docker("start", cid)
            info = json.loads(docker("inspect", cid))[0]
            port = info["NetworkSettings"]["Ports"]["80/tcp"][0]["HostPort"]
            base = f"http://127.0.0.1:{port}"

            def request(path, method="GET", headers=None):
                req = urllib.request.Request(base + path, method=method, headers=headers or {})
                try:
                    return urllib.request.urlopen(req, timeout=3)
                except urllib.error.HTTPError as error:
                    return error

            for attempt in range(30):
                try:
                    with request("/") as response:
                        assert response.status == 200
                    break
                except (urllib.error.URLError, ConnectionError):
                    if attempt == 29:
                        raise
                    time.sleep(0.1)
            # Embed surface is hardened off: every embed/widget path must 404
            # even though the files exist in the webroot, and must never fall
            # through to the SPA or leak the entry file.
            embed_paths = (
                "/embed/active", "/embed/active?token=ignored",
                "/embed/disabled", "/embed/empty", "/embed/unavailable",
                "/embed.html", "/weknora-widget.js",
            )
            for method in ("GET", "HEAD"):
                for path in embed_paths:
                    with request(path, method) as response:
                        assert response.status == 404, (path, response.status)
                        assert b"embed entry" not in response.read(), path
            with request("/api/probe") as response:
                assert response.read().decode() == f"127.0.0.1:{port}", "proxy lost public port"
            for method in ("POST", "GET", "DELETE"):
                with request("/mcp/test-endpoint?probe=1", method, {
                    "Authorization": "Bearer test-token",
                    "Mcp-Session-Id": "test-session",
                    "MCP-Protocol-Version": "2025-03-26",
                }) as response:
                    assert response.status == 200, (method, response.status)
                    assert response.headers["Mcp-Session-Id"] == "test-session"
                    assert json.load(response) == {
                        "method": method, "path": "/mcp/test-endpoint?probe=1",
                        "host": f"127.0.0.1:{port}", "session": "test-session",
                        "protocol": "2025-03-26",
                    }
            with request("/mcp/test-endpoint") as response:
                assert response.status == 401, response.status
            with request("/mcp/stream") as response:
                assert response.headers["Content-Type"] == "text/event-stream"
                assert response.read() == b"event: message\ndata: test-event\n\n"
            print("Embed hardening and MCP Nginx integration checks passed")
        except Exception:
            print(docker("logs", cid), file=sys.stderr)
            raise
        finally:
            docker("rm", "-f", cid)


if __name__ == "__main__":
    check()
