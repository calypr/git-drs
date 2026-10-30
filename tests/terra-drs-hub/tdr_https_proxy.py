#!/usr/bin/env python3
"""Forward Hub's HTTPS DRS calls to the local HTTP TDR test server."""

import http.client
import json
import ssl
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit


upstream = urlsplit(sys.argv[1])
listen_port = int(sys.argv[2])
certificate = sys.argv[3]
private_key = sys.argv[4]
request_log = sys.argv[5]


class ProxyHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_OPTIONS(self):
        self.forward()

    def do_GET(self):
        self.forward()

    def do_POST(self):
        self.forward()

    def do_HEAD(self):
        self.forward()

    def forward(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        request = {
            "method": self.command,
            "path": self.path,
            "authorization": self.headers.get("Authorization", ""),
        }
        connection = http.client.HTTPConnection(upstream.hostname, upstream.port, timeout=30)
        try:
            headers = {
                name: value
                for name, value in self.headers.items()
                if name.lower() not in {"host", "connection", "content-length"}
            }
            connection.request(self.command, self.path, body=body, headers=headers)
            response = connection.getresponse()
            request["status"] = response.status
            response_body = response.read()
            self.send_response(response.status, response.reason)
            for name, value in response.getheaders():
                if name.lower() not in {"connection", "transfer-encoding", "content-length"}:
                    self.send_header(name, value)
            self.send_header("Content-Length", str(len(response_body)))
            self.send_header("Connection", "close")
            self.end_headers()
            if self.command != "HEAD":
                self.wfile.write(response_body)
        except Exception as error:  # Return the forwarding failure to the real Hub client.
            request["status"] = 502
            response_body = str(error).encode("utf-8")
            self.send_response(502)
            self.send_header("Content-Length", str(len(response_body)))
            self.send_header("Connection", "close")
            self.end_headers()
            self.wfile.write(response_body)
        finally:
            with open(request_log, "a", encoding="utf-8") as log:
                log.write(json.dumps(request) + "\n")
            connection.close()

    def log_message(self, format, *args):
        print(format % args, file=sys.stderr)


server = ThreadingHTTPServer(("127.0.0.1", listen_port), ProxyHandler)
context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
context.load_cert_chain(certificate, private_key)
server.socket = context.wrap_socket(server.socket, server_side=True)
server.serve_forever()
