#!/usr/bin/env python3
"""
Simple mock receiver for testing keep_src functionality
"""
import http.server
import socketserver
import json
import sys
from urllib.parse import urlparse

class MockReceiverHandler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        # Parse the URL to extract tenant and dataset
        parsed = urlparse(self.path)
        path_parts = parsed.path.strip('/').split('/')

        if len(path_parts) >= 2:
            tenant = path_parts[0]
            dataset = path_parts[1]
        else:
            tenant = "unknown"
            dataset = "unknown"

        # Read the content
        content_length = int(self.headers.get('Content-Length', 0))
        content = self.rfile.read(content_length) if content_length > 0 else b''

        # Log the request details
        print(f"\n=== Received upload ===")
        print(f"Path: {self.path}")
        print(f"Tenant: {tenant}, Dataset: {dataset}")
        print(f"Content-Length: {content_length}")
        print(f"Headers:")
        for key, value in self.headers.items():
            if key.startswith('X-Proxy'):
                print(f"  {key}: {value}")
        print(f"Data size: {len(content)} bytes")
        print(f"======================")

        # Send success response
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()

        response = {
            "status": "success",
            "tenant": tenant,
            "dataset": dataset,
            "received_bytes": len(content)
        }
        self.wfile.write(json.dumps(response).encode())

    def log_message(self, format, *args):
        # Suppress default logging
        pass

if __name__ == "__main__":
    PORT = 9091

    print(f"Starting mock receiver on port {PORT}")
    print("Press Ctrl+C to stop")

    with socketserver.TCPServer(("", PORT), MockReceiverHandler) as httpd:
        try:
            httpd.serve_forever()
        except KeyboardInterrupt:
            print("\nShutting down mock receiver")
            sys.exit(0)